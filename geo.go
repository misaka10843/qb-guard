package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	mmdbMarker       = "\xab\xcd\xefMaxMind.com"
	mmdbDataSepSize  = 16
	mmdbMaxSize      = 64 << 20
	mmdbMetaScanSize = 128 << 10
)

type mmdb struct {
	data       []byte
	nodeCount  int
	recordSize int
	ipVersion  int
	treeSize   int
	dataBase   int
	ipv4Start  int
	builtAt    time.Time
	dbType     string
}

func openMMDB(data []byte) (*mmdb, error) {
	scanFrom := len(data) - mmdbMetaScanSize
	if scanFrom < 0 {
		scanFrom = 0
	}
	idx := bytes.LastIndex(data[scanFrom:], []byte(mmdbMarker))
	if idx < 0 {
		return nil, errors.New("找不到 MaxMind 元数据标记，不是 mmdb 文件")
	}
	metaStart := scanFrom + idx + len(mmdbMarker)

	raw, _, err := (&mmdb{data: data, dataBase: metaStart}).value(metaStart)
	if err != nil {
		return nil, fmt.Errorf("解析元数据失败: %w", err)
	}
	meta, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("元数据不是映射结构")
	}
	m := &mmdb{data: data}
	m.nodeCount = toInt(meta["node_count"])
	m.recordSize = toInt(meta["record_size"])
	m.ipVersion = toInt(meta["ip_version"])
	m.dbType, _ = meta["database_type"].(string)
	if epoch := toInt64(meta["build_epoch"]); epoch > 0 {
		m.builtAt = time.Unix(epoch, 0)
	}
	if m.nodeCount <= 0 {
		return nil, errors.New("元数据里的 node_count 无效")
	}
	if m.recordSize != 24 && m.recordSize != 28 && m.recordSize != 32 {
		return nil, fmt.Errorf("不支持的 record_size %d", m.recordSize)
	}
	if m.ipVersion != 4 && m.ipVersion != 6 {
		return nil, fmt.Errorf("不支持的 ip_version %d", m.ipVersion)
	}
	m.treeSize = m.nodeCount * m.recordSize * 2 / 8
	m.dataBase = m.treeSize + mmdbDataSepSize
	if m.dataBase > len(data) {
		return nil, errors.New("搜索树长度超出文件，文件可能被截断")
	}
	m.ipv4Start = m.findIPv4Start()
	return m, nil
}

func (m *mmdb) findIPv4Start() int {
	if m.ipVersion != 6 {
		return 0
	}
	node := 0
	for i := 0; i < 96 && node < m.nodeCount; i++ {
		node = m.record(node, 0)
	}
	if node >= m.nodeCount {
		return 0
	}
	return node
}

func (m *mmdb) record(node, side int) int {
	offset := node * m.recordSize * 2 / 8
	switch m.recordSize {
	case 24:
		at := offset + side*3
		return int(m.data[at])<<16 | int(m.data[at+1])<<8 | int(m.data[at+2])
	case 28:
		if side == 0 {
			return int(m.data[offset+3]&0xF0)<<20 | int(m.data[offset])<<16 |
				int(m.data[offset+1])<<8 | int(m.data[offset+2])
		}
		return int(m.data[offset+3]&0x0F)<<24 | int(m.data[offset+4])<<16 |
			int(m.data[offset+5])<<8 | int(m.data[offset+6])
	default:
		at := offset + side*4
		return int(binary.BigEndian.Uint32(m.data[at : at+4]))
	}
}

func (m *mmdb) Country(addr netip.Addr) string {
	addr = addr.Unmap()
	var (
		node  int
		bytes []byte
	)
	switch {
	case addr.Is4():
		if m.ipVersion == 6 {
			node = m.ipv4Start
		}
		raw := addr.As4()
		bytes = raw[:]
	case addr.Is6():
		if m.ipVersion == 4 {
			return ""
		}
		raw := addr.As16()
		bytes = raw[:]
	default:
		return ""
	}
	for _, by := range bytes {
		for shift := 7; shift >= 0; shift-- {
			node = m.record(node, int(by>>uint(shift))&1)
			if node == m.nodeCount {
				return ""
			}
			if node > m.nodeCount {
				return m.countryAt(node - m.nodeCount + m.treeSize)
			}
		}
	}
	return ""
}

func (m *mmdb) countryAt(offset int) string {
	raw, _, err := m.value(offset)
	if err != nil {
		return ""
	}
	top, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	country, ok := top["country"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := country["iso_code"].(string)
	return code
}

func (m *mmdb) value(pos int) (any, int, error) {
	if pos >= len(m.data) {
		return nil, pos, errors.New("数据段偏移越界")
	}
	ctrl := m.data[pos]
	pos++
	kind := int(ctrl >> 5)
	if kind == 0 {
		if pos >= len(m.data) {
			return nil, pos, errors.New("扩展类型缺少类型字节")
		}
		kind = int(m.data[pos]) + 7
		pos++
	}
	size := int(ctrl & 0x1F)
	if size >= 29 && kind != 1 {
		extra := size - 28
		if pos+extra > len(m.data) {
			return nil, pos, errors.New("长度字段越界")
		}
		size = int(beUint(m.data[pos:pos+extra])) + []int{0, 29, 285, 65821}[extra]
		pos += extra
	}
	start := pos

	need := 0
	switch kind {
	case 2, 4, 5, 6, 8, 9, 10:
		need = size
	case 3:
		need = 8
	case 15:
		need = 4
	}
	if start+need > len(m.data) {
		return nil, pos, errors.New("数据段越界")
	}

	switch kind {
	case 1:
		ptr, next, err := m.pointer(ctrl, pos)
		if err != nil {
			return nil, pos, err
		}
		value, _, err := m.value(m.dataBase + ptr)
		return value, next, err
	case 2:
		return string(m.data[start : start+size]), start + size, nil
	case 3:
		return math.Float64frombits(beUint(m.data[start : start+8])), start + 8, nil
	case 4:
		return m.data[start : start+size], start + size, nil
	case 5, 6, 9, 10:
		return int(beUint(m.data[start : start+size])), start + size, nil
	case 7:
		out := make(map[string]any, size)
		pos = start
		for i := 0; i < size; i++ {
			key, next, err := m.value(pos)
			if err != nil {
				return nil, pos, err
			}
			value, next, err := m.value(next)
			if err != nil {
				return nil, pos, err
			}
			pos = next
			if name, ok := key.(string); ok {
				out[name] = value
			}
		}
		return out, pos, nil
	case 8:
		return int(int64(beUint(m.data[start : start+size]))), start + size, nil
	case 11:
		out := make([]any, 0, size)
		pos = start
		for i := 0; i < size; i++ {
			value, next, err := m.value(pos)
			if err != nil {
				return nil, pos, err
			}
			out = append(out, value)
			pos = next
		}
		return out, pos, nil
	case 13:
		return nil, start, nil
	case 14:
		return size != 0, start, nil
	case 15:
		return float64(math.Float32frombits(uint32(beUint(m.data[start : start+4])))), start + 4, nil
	}
	return nil, pos, fmt.Errorf("未知的数据类型 %d", kind)
}

func (m *mmdb) pointer(ctrl byte, pos int) (int, int, error) {
	n := (int(ctrl&0x1F) >> 3) + 1
	if pos+n > len(m.data) {
		return 0, pos, errors.New("指针字段越界")
	}
	ptr := int(beUint(m.data[pos : pos+n]))
	switch n {
	case 1:
		ptr |= int(ctrl&0x7) << 8
	case 2:
		ptr |= int(ctrl&0x7) << 16
		ptr += 2048
	case 3:
		ptr |= int(ctrl&0x7) << 24
		ptr += 526336
	}
	return ptr, pos + n, nil
}

func beUint(b []byte) uint64 {
	var out uint64
	for _, x := range b {
		out = out<<8 | uint64(x)
	}
	return out
}

func toInt(v any) int {
	n, _ := v.(int)
	return n
}

func toInt64(v any) int64 {
	n, _ := v.(int)
	return int64(n)
}

const defaultGeoMMDBURL = "https://gh.huoshen80.top/github.com/MetaCubeX/meta-rules-dat/releases/download/latest/country-lite.mmdb"

type geoConfig struct {
	Enabled         bool     `yaml:"enabled"`
	MMDBURL         string   `yaml:"mmdb-url"`
	RefreshInterval Duration `yaml:"refresh-interval"`
	HistoryKeep     Duration `yaml:"history-keep"`
}

func (c geoConfig) interval() time.Duration {
	if c.RefreshInterval > 0 {
		return time.Duration(c.RefreshInterval)
	}
	return 7 * 24 * time.Hour
}

func (c geoConfig) keep() time.Duration {
	if c.HistoryKeep > 0 {
		return time.Duration(c.HistoryKeep)
	}
	return 90 * 24 * time.Hour
}

type geoStatus struct {
	Enabled   bool   `json:"enabled"`
	Ready     bool   `json:"ready"`
	Database  string `json:"database,omitempty"`
	BuiltAt   string `json:"builtAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	Size      int64  `json:"size"`
	URL       string `json:"url"`
	LastError string `json:"lastError,omitempty"`
	Running   bool   `json:"running"`
}

type geoDB struct {
	dir    string
	http   *http.Client
	notify func()

	refreshMu sync.Mutex

	mu      sync.RWMutex
	mmdb    *mmdb
	size    int64
	updated time.Time
	lastErr string
	running bool
}

func newGeoDB(dir string) *geoDB {
	return &geoDB{dir: dir, http: &http.Client{Timeout: 120 * time.Second}}
}

func (g *geoDB) path() string { return filepath.Join(g.dir, "country.mmdb") }

func (g *geoDB) Load(cfg geoConfig) {
	if !cfg.Enabled {
		return
	}
	data, err := os.ReadFile(g.path())
	if err != nil {
		logf("地理库缓存不存在，开始首次下载")
		g.Refresh(cfg)
		return
	}
	if err := g.install(data); err != nil {
		g.recordErr(err)
		logf("地理库缓存无法解析（%v），重新下载", err)
		g.Refresh(cfg)
		return
	}
	g.updated = modTime(g.path())
	logf("地理库已载入: %s，%d 字节", g.databaseName(), len(data))
}

func (g *geoDB) Refresh(cfg geoConfig) bool {
	if !cfg.Enabled {
		return false
	}
	if !g.refreshMu.TryLock() {
		return false
	}
	g.setRunning(true)
	go func() {
		defer g.refreshMu.Unlock()
		defer g.setRunning(false)
		if err := g.download(cfg.MMDBURL); err != nil {
			g.recordErr(err)
			logf("地理库下载失败: %v", err)
		}
		g.fire()
	}()
	return true
}

func (g *geoDB) download(rawURL string) error {
	resp, err := g.http.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, mmdbMaxSize))
	if err != nil {
		return err
	}
	if err := g.install(data); err != nil {
		return fmt.Errorf("下载内容不是有效的 mmdb: %w", err)
	}
	if err := os.MkdirAll(g.dir, 0o755); err != nil {
		return err
	}
	if err := writeAtomic(g.path(), data); err != nil {
		return err
	}
	g.mu.Lock()
	g.size = int64(len(data))
	g.updated = time.Now()
	g.lastErr = ""
	g.mu.Unlock()
	logf("地理库已更新: %s，%d 字节", g.databaseName(), len(data))
	return nil
}

func (g *geoDB) install(data []byte) error {
	parsed, err := openMMDB(data)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.mmdb = parsed
	g.size = int64(len(data))
	g.lastErr = ""
	g.mu.Unlock()
	return nil
}

func (g *geoDB) Country(addr netip.Addr) string {
	g.mu.RLock()
	reader := g.mmdb
	g.mu.RUnlock()
	if reader == nil {
		return ""
	}
	return reader.Country(addr)
}

func (g *geoDB) databaseName() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.mmdb == nil {
		return ""
	}
	return g.mmdb.dbType
}

func (g *geoDB) Status(cfg geoConfig) geoStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()
	status := geoStatus{
		Enabled:   cfg.Enabled,
		Ready:     g.mmdb != nil,
		Database:  g.mmdb.dbTypeOrEmpty(),
		Size:      g.size,
		URL:       cfg.MMDBURL,
		LastError: g.lastErr,
		Running:   g.running,
	}
	if g.mmdb != nil && !g.mmdb.builtAt.IsZero() {
		status.BuiltAt = g.mmdb.builtAt.Format(time.RFC3339)
	}
	if !g.updated.IsZero() {
		status.UpdatedAt = g.updated.Format(time.RFC3339)
	}
	return status
}

func (m *mmdb) dbTypeOrEmpty() string {
	if m == nil {
		return ""
	}
	return m.dbType
}

func (g *geoDB) setRunning(v bool) {
	g.mu.Lock()
	g.running = v
	g.mu.Unlock()
}

func (g *geoDB) recordErr(err error) {
	g.mu.Lock()
	g.lastErr = err.Error()
	g.mu.Unlock()
}

func (g *geoDB) fire() {
	if g.notify != nil {
		g.notify()
	}
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func (g *geoDB) loop(cfg func() geoConfig, stop <-chan struct{}) {
	timer := time.NewTimer(cfg().interval())
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			current := cfg()
			if current.Enabled {
				g.Refresh(current)
			}
			timer.Reset(current.interval())
		case <-stop:
			return
		}
	}
}

var geoRanges = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour,
	"all": 0,
}

const geoIPLimit = 40

func (a *App) handleGeo(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Load().Geo
	key := r.URL.Query().Get("range")
	if key == "" {
		key = "7d"
	}
	window, ok := geoRanges[key]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "range 只能是 24h / 7d / 30d / 90d / all"})
		return
	}
	var cutoff int64
	if window > 0 {
		cutoff = time.Now().Add(-window).Unix()
	}
	agg := a.banLog.aggregate(cutoff, a.geo.Country, geoIPLimit)
	writeJSON(w, http.StatusOK, map[string]any{
		"ts":          time.Now().Unix(),
		"range":       key,
		"database":    a.geo.Status(cfg),
		"total":       agg.Total,
		"unknown":     agg.Unknown,
		"countries":   agg.Buckets,
		"series":      agg.Days,
		"historyKeep": formatDuration(cfg.keep()),
		"logged":      a.banLog.count(),
	})
}

func (a *App) handleGeoRefresh(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Load().Geo
	if !cfg.Enabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "地理位置功能未启用"})
		return
	}
	if !a.geo.Refresh(cfg) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "更新正在进行中"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true, "running": true})
}
