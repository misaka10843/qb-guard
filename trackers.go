package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type trackerSource struct {
	ID      string
	Name    string `yaml:"name"`
	URL     string `yaml:"url"`
	Enabled bool   `yaml:"enabled"`
}

type trackersConfig struct {
	Enabled         bool                     `yaml:"enabled"`
	RefreshInterval Duration                 `yaml:"refresh-interval"`
	Sources         map[string]trackerSource `yaml:"sources"`
}

func (c trackersConfig) interval() time.Duration {
	if c.RefreshInterval > 0 {
		return time.Duration(c.RefreshInterval)
	}
	return 24 * time.Hour
}

func (c trackersConfig) sources() []trackerSource {
	ids := make([]string, 0, len(c.Sources))
	for id := range c.Sources {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]trackerSource, 0, len(ids))
	for _, id := range ids {
		src := c.Sources[id]
		src.ID = id
		if src.Name == "" {
			src.Name = id
		}
		out = append(out, src)
	}
	return out
}

func (c trackersConfig) validate() error {
	for _, src := range c.sources() {
		if !src.Enabled {
			continue
		}
		u, err := url.Parse(src.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("trackers.sources.%s 的 url 必须是 http/https 地址，实际 %q", src.ID, src.URL)
		}
	}
	return nil
}

var trackerSchemes = map[string]bool{"http": true, "https": true, "udp": true, "ws": true, "wss": true}

func parseTrackers(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Host == "" || !trackerSchemes[strings.ToLower(u.Scheme)] {
			continue
		}
		out = append(out, line)
	}
	return out
}

type trackerStat struct {
	Count int `json:"count"`
	Added int `json:"added"`
}

type trackerSnapshot struct {
	Total    int                    `json:"total"`
	List     []string               `json:"list"`
	Stats    map[string]trackerStat `json:"stats"`
	LastRun  string                 `json:"lastRun,omitempty"`
	PushedAt string                 `json:"pushedAt,omitempty"`
	LastErr  string                 `json:"lastError,omitempty"`
	Running  bool                   `json:"running"`
}

type trackerSet struct {
	dir    string
	http   *http.Client
	qb     *QBClient
	notify func()

	refreshMu sync.Mutex
	pushMu    sync.Mutex

	mu         sync.RWMutex
	merged     []string
	stats      map[string]trackerStat
	lastRun    time.Time
	lastErr    string
	pushedAt   time.Time
	pushedHash string
	running    bool
}

func newTrackerSet(dir string, qb *QBClient) *trackerSet {
	return &trackerSet{dir: dir, http: &http.Client{Timeout: 60 * time.Second}, qb: qb}
}

func (t *trackerSet) Refresh(cfg trackersConfig) bool {
	if !t.refreshMu.TryLock() {
		return false
	}
	t.setRunning(true)
	go func() {
		defer t.refreshMu.Unlock()
		defer t.setRunning(false)
		t.run(cfg)
	}()
	return true
}

func (t *trackerSet) Load(cfg trackersConfig) {
	merged, stats, newest, missing := t.mergeCache(cfg)
	if missing || len(merged) == 0 {
		logf("tracker 缓存不完整，开始首次拉取")
		t.Refresh(cfg)
		return
	}
	t.publish(merged, stats, newest)
	if err := t.push(merged); err != nil {
		t.recordErr(err)
		logf("写入下载器 tracker 列表失败: %v", err)
	}
	t.fire()
}

func (t *trackerSet) run(cfg trackersConfig) {
	if err := os.MkdirAll(t.dir, 0o755); err != nil {
		t.recordErr(fmt.Errorf("创建缓存目录失败: %w", err))
		t.fire()
		return
	}

	var (
		seen      = map[string]struct{}{}
		merged    []string
		stats     = map[string]trackerStat{}
		failed    []string
		fetched   int
		attempted int
	)
	for _, src := range cfg.sources() {
		if !src.Enabled || src.URL == "" {
			continue
		}
		attempted++
		path := filepath.Join(t.dir, src.ID+".txt")
		data, err := t.fetch(src.URL)
		switch {
		case err != nil:
			cached, readErr := os.ReadFile(path)
			if readErr != nil {
				logf("tracker 源 %s 拉取失败且无缓存: %v", src.ID, err)
				failed = append(failed, src.ID)
				continue
			}
			logf("tracker 源 %s 拉取失败，沿用缓存: %v", src.ID, err)
			data = cached
		default:
			fetched++
			if cached, readErr := os.ReadFile(path); readErr != nil || sha256Hex(cached) != sha256Hex(data) {
				if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
					logf("tracker 源 %s 写入缓存失败: %v", src.ID, writeErr)
				}
			}
		}
		list := parseTrackers(string(data))
		added := 0
		for _, tr := range list {
			key := strings.ToLower(tr)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, tr)
			added++
		}
		stats[src.ID] = trackerStat{Count: len(list), Added: added}
	}

	t.publish(merged, stats, time.Now())
	if len(failed) > 0 {
		t.recordErr(fmt.Errorf("源 %s 拉取失败且无本地缓存", strings.Join(failed, "、")))
	}
	if len(merged) == 0 {
		if attempted > 0 {
			t.recordErr(fmt.Errorf("所有启用源都没有取到 tracker，保持下载器现有列表不变"))
		}
		t.fire()
		return
	}
	if err := t.push(merged); err != nil {
		t.recordErr(err)
		logf("写入下载器 tracker 列表失败: %v", err)
	} else {
		logf("tracker 已更新：%d 个源取到 %d 条，去重后 %d 条", fetched, countParsed(stats), len(merged))
	}
	t.fire()
}

func countParsed(stats map[string]trackerStat) int {
	n := 0
	for _, s := range stats {
		n += s.Count
	}
	return n
}

func (t *trackerSet) fetch(rawURL string) ([]byte, error) {
	resp, err := t.http.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func (t *trackerSet) push(list []string) error {
	joined := strings.Join(list, "\n")
	hash := sha256Hex([]byte(joined))

	t.mu.RLock()
	same := hash == t.pushedHash
	t.mu.RUnlock()
	if same {
		return nil
	}

	t.pushMu.Lock()
	defer t.pushMu.Unlock()
	if err := t.qb.SetAddTrackers(joined); err != nil {
		return err
	}
	t.mu.Lock()
	t.pushedHash = hash
	t.pushedAt = time.Now()
	t.mu.Unlock()
	return nil
}

func (t *trackerSet) mergeCache(cfg trackersConfig) ([]string, map[string]trackerStat, time.Time, bool) {
	var (
		seen   = map[string]struct{}{}
		merged []string
		stats  = map[string]trackerStat{}
		newest time.Time
		miss   bool
	)
	for _, src := range cfg.sources() {
		if !src.Enabled || src.URL == "" {
			continue
		}
		path := filepath.Join(t.dir, src.ID+".txt")
		info, statErr := os.Stat(path)
		data, err := os.ReadFile(path)
		if err != nil {
			miss = true
			continue
		}
		if statErr == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		list := parseTrackers(string(data))
		added := 0
		for _, tr := range list {
			key := strings.ToLower(tr)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, tr)
			added++
		}
		stats[src.ID] = trackerStat{Count: len(list), Added: added}
	}
	return merged, stats, newest, miss
}

func (t *trackerSet) publish(merged []string, stats map[string]trackerStat, at time.Time) {
	t.mu.Lock()
	t.merged = merged
	t.stats = stats
	t.lastRun = at
	t.lastErr = ""
	t.mu.Unlock()
}

func (t *trackerSet) recordErr(err error) {
	t.mu.Lock()
	t.lastErr = err.Error()
	t.mu.Unlock()
}

func (t *trackerSet) setRunning(v bool) {
	t.mu.Lock()
	t.running = v
	t.mu.Unlock()
}

func (t *trackerSet) fire() {
	if t.notify != nil {
		t.notify()
	}
}

func (t *trackerSet) isRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}

func (t *trackerSet) due(interval time.Duration) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.running {
		return false
	}
	if t.lastErr != "" {
		return time.Since(t.lastRun) >= 15*time.Minute
	}
	return t.lastRun.IsZero() || time.Since(t.lastRun) >= interval
}

func (t *trackerSet) snapshot() trackerSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	snap := trackerSnapshot{
		Total:   len(t.merged),
		List:    t.merged,
		Stats:   t.stats,
		LastErr: t.lastErr,
		Running: t.running,
	}
	if !t.lastRun.IsZero() {
		snap.LastRun = t.lastRun.Format(time.RFC3339)
	}
	if !t.pushedAt.IsZero() {
		snap.PushedAt = t.pushedAt.Format(time.RFC3339)
	}
	return snap
}

func (a *App) trackersLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.tickTrackers()
		case <-stop:
			return
		}
	}
}

func (a *App) tickTrackers() {
	cfg := a.cfg.Load().Trackers
	if !cfg.Enabled {
		return
	}
	if a.trackers.due(cfg.interval()) {
		a.trackers.Refresh(cfg)
	}
}

func (a *App) handleTrackers(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Load().Trackers
	snap := a.trackers.snapshot()

	sources := []any{}
	for _, src := range cfg.sources() {
		sources = append(sources, map[string]any{
			"id": src.ID, "name": src.Name, "url": src.URL, "enabled": src.Enabled,
			"count": snap.Stats[src.ID].Count, "added": snap.Stats[src.ID].Added,
		})
	}
	out := map[string]any{
		"enabled":         cfg.Enabled,
		"refreshInterval": formatDuration(cfg.interval()),
		"sources":         sources,
		"snapshot":        snap,
	}
	if prefs, err := a.qb.Preferences(); err == nil {
		raw, _ := prefs["add_trackers"].(string)
		out["downloader"] = map[string]any{
			"addTrackersCount":   countLines(raw),
			"addTrackersEnabled": prefs["add_trackers_enabled"],
			"addTrackersFromURL": prefs["add_trackers_from_url_enabled"],
			"addTrackersURL":     prefs["add_trackers_url"],
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleRefreshTrackers(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Load().Trackers
	if !cfg.Enabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tracker 订阅聚合未启用"})
		return
	}
	if !a.trackers.Refresh(cfg) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "刷新正在进行中"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true, "running": true})
}

func countLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

var defaultTrackerSources = map[string]trackerSource{
	"xiu2-best": {
		Name: "XIU2 精选",
		URL:  "https://gh.huoshen80.top/https://raw.githubusercontent.com/XIU2/TrackersListCollection/master/best.txt",
	},
	"ngosang-best": {
		Name: "ngosang 精选",
		URL:  "https://gh.huoshen80.top/https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt",
	},
}

func defaultTrackersConfig() trackersConfig {
	sources := make(map[string]trackerSource, len(defaultTrackerSources))
	for id, src := range defaultTrackerSources {
		src.Enabled = true
		sources[id] = src
	}
	return trackersConfig{RefreshInterval: Duration(24 * time.Hour), Sources: sources}
}

func (q *QBClient) SetAddTrackers(list string) error {
	payload, err := json.Marshal(map[string]any{
		"add_trackers":         list,
		"add_trackers_enabled": true,
	})
	if err != nil {
		return err
	}
	return q.do(http.MethodPost, "/api/v2/app/setPreferences", url.Values{"json": {string(payload)}}, nil)
}
