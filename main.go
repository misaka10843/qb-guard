
package main

import (
	"embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	_ "time/tzdata"

	"gopkg.in/yaml.v3"
)

//go:embed all:webui/dist
var webuiAssets embed.FS

func logf(format string, args ...any) {
	log.Printf(format, args...)
}

const version = "1.0.0"

func loadInt64(p *int64) int64 { return atomic.LoadInt64(p) }

type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	switch value.Tag {
	case "!!int":
		var ms int64
		if err := value.Decode(&ms); err != nil {
			return err
		}
		*d = Duration(time.Duration(ms) * time.Millisecond)
		return nil
	case "!!str":
		var s string
		if err := value.Decode(&s); err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(s), "default") {
			*d = 0
			return nil
		}
		parsed, err := parseDuration(s)
		if err != nil {
			return err
		}
		*d = Duration(parsed)
		return nil
	case "!!null":
		*d = 0
		return nil
	default:
		return fmt.Errorf("时长 %q 无法解析：应为毫秒整数或 Go 时长字符串（如 5s、72h、7d）", value.Value)
	}
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n := len(s); n > 1 {
		switch s[n-1] {
		case 'd', 'D':
			if v, err := strconv.ParseFloat(s[:n-1], 64); err == nil {
				return time.Duration(v * float64(24*time.Hour)), nil
			}
		case 'w', 'W':
			if v, err := strconv.ParseFloat(s[:n-1], 64); err == nil {
				return time.Duration(v * float64(7*24*time.Hour)), nil
			}
		}
	}
	return time.ParseDuration(s)
}

func formatDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "0s"
	case d%(7*24*time.Hour) == 0:
		return strconv.FormatInt(int64(d/(7*24*time.Hour)), 10) + "w"
	case d%(24*time.Hour) == 0:
		return strconv.FormatInt(int64(d/(24*time.Hour)), 10) + "d"
	default:
		return d.String()
	}
}

func (d Duration) MarshalYAML() (any, error) {
	return formatDuration(time.Duration(d)), nil
}

type QBConfig struct {
	URL       string `yaml:"url"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
	BanMethod string `yaml:"ban-method"`
}

type baseModule struct {
	Enabled     bool     `yaml:"enabled"`
	BanDuration Duration `yaml:"ban-duration"`
}

type peerIDBlacklistConfig struct {
	baseModule   `yaml:",inline"`
	BannedPeerID []string `yaml:"banned-peer-id"`
}

type clientNameBlacklistConfig struct {
	baseModule       `yaml:",inline"`
	BannedClientName []string `yaml:"banned-client-name"`
}

type ipBlacklistConfig struct {
	baseModule `yaml:",inline"`
	IPs        []string `yaml:"ips"`
	Ports      []int    `yaml:"ports"`
}

type pcbConfig struct {
	baseModule       `yaml:",inline"`
	MinimumSize      int64    `yaml:"minimum-size"`
	BlockExcessive   bool     `yaml:"block-excessive-clients"`
	Excessive        float64  `yaml:"excessive-threshold"`
	MaxDifference    float64  `yaml:"maximum-difference"`
	RewindMaxDiff    float64  `yaml:"rewind-maximum-difference"`
	IPv4PrefixLength int      `yaml:"ipv4-prefix-length"`
	IPv6PrefixLength int      `yaml:"ipv6-prefix-length"`
	PersistDuration  Duration `yaml:"persist-duration"`
	MaxWaitDuration  Duration `yaml:"max-wait-duration"`
	FastTestPercent  float64  `yaml:"fast-pcb-test-percentage"`
	FastTestBlockDur Duration `yaml:"fast-pcb-test-block-duration"`
}

type autoRangeBanConfig struct {
	baseModule `yaml:",inline"`
	IPv4       int `yaml:"ipv4"`
	IPv6       int `yaml:"ipv6"`
}

type multiDialingConfig struct {
	baseModule      `yaml:",inline"`
	SubnetMask      int      `yaml:"subnet-mask-length"`
	SubnetMaskV6    int      `yaml:"subnet-mask-v6-length"`
	TolerateV4      int      `yaml:"tolerate-num-ipv4"`
	TolerateV6      int      `yaml:"tolerate-num-ipv6"`
	CacheLifespan   Duration `yaml:"cache-lifespan"`
	KeepHunting     bool     `yaml:"keep-hunting"`
	KeepHuntingTime Duration `yaml:"keep-hunting-time"`
}

type expressionConfig struct {
	baseModule `yaml:",inline"`
}

type ipRuleListConfig struct {
	baseModule    `yaml:",inline"`
	CheckInterval Duration             `yaml:"check-interval"`
	Rules         map[string]ipRuleSub `yaml:"rules"`
}

func (c *ipRuleListConfig) subscriptions() []ipRuleSub {
	ids := make([]string, 0, len(c.Rules))
	for id := range c.Rules {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	subs := make([]ipRuleSub, 0, len(ids))
	for _, id := range ids {
		sub := c.Rules[id]
		sub.ID = id
		if sub.Name == "" {
			sub.Name = id
		}
		subs = append(subs, sub)
	}
	return subs
}

type activeMonitoringConfig struct {
	baseModule `yaml:",inline"`
	Traffic    struct {
		Daily int64 `yaml:"daily"`
	} `yaml:"traffic-monitoring"`
}

type ModulesConfig struct {
	PeerIDBlacklist     *peerIDBlacklistConfig     `yaml:"peer-id-blacklist"`
	ClientNameBlacklist *clientNameBlacklistConfig `yaml:"client-name-blacklist"`
	IPBlacklist         *ipBlacklistConfig         `yaml:"ip-address-blocker"`
	PCB                 *pcbConfig                 `yaml:"progress-cheat-blocker"`
	AutoRangeBan        *autoRangeBanConfig        `yaml:"auto-range-ban"`
	MultiDialing        *multiDialingConfig        `yaml:"multi-dialing-blocker"`
	Expression          *expressionConfig          `yaml:"expression-engine"`
	IPRuleList          *ipRuleListConfig          `yaml:"ip-address-blocker-rules"`
	ActiveMonitoring    *activeMonitoringConfig    `yaml:"active-monitoring"`
}

type statsConfig struct {
	SampleInterval Duration `yaml:"sample-interval"`
	RecentKeep     Duration `yaml:"recent-keep"`
	HourlyKeep     Duration `yaml:"hourly-keep"`
	DailyKeep      Duration `yaml:"daily-keep"`
}

func (c statsConfig) window() statsWindow {
	window := statsWindow{
		recent: time.Duration(c.RecentKeep),
		hourly: time.Duration(c.HourlyKeep),
		daily:  time.Duration(c.DailyKeep),
	}
	if window.recent <= 0 {
		window.recent = 48 * time.Hour
	}
	if window.hourly <= 0 {
		window.hourly = 30 * 24 * time.Hour
	}
	if window.daily <= 0 {
		window.daily = 365 * 24 * time.Hour
	}
	return window
}

type serverConfig struct {
	Address   string `yaml:"address"`
	HTTP      int    `yaml:"http"`
	AllowCORS bool   `yaml:"allow-cors"`
}

func (s serverConfig) listenAddr() string {
	host := s.Address
	if host == "" {
		host = "0.0.0.0"
	}
	port := s.HTTP
	if port <= 0 {
		port = 9091
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

type Config struct {
	QB              QBConfig       `yaml:"qbittorrent"`
	Server          serverConfig   `yaml:"server"`
	CheckInterval   Duration       `yaml:"check-interval"`
	BanDuration     Duration       `yaml:"ban-duration"`
	IgnoreAddresses []string       `yaml:"ignore-peers-from-addresses"`
	DataDir         string         `yaml:"data-dir"`
	Concurrency     int            `yaml:"concurrency"`
	PersistInterval Duration       `yaml:"persist-interval"`
	Stats           statsConfig    `yaml:"stats"`
	Trackers        trackersConfig `yaml:"trackers"`
	Modules         ModulesConfig  `yaml:"module"`
}

var renamedKeys = map[string]string{
	"modules":          "module",
	"ignore-addresses": "ignore-peers-from-addresses",
	"listen":           "server.address + server.http",
}

var renamedRuleKeys = map[string]string{
	"peer-id-blacklist":     "banned-peer-id",
	"client-name-blacklist": "banned-client-name",
}

var knownTopKeys = []string{
	"qbittorrent", "server", "check-interval", "ban-duration", "ignore-peers-from-addresses",
	"data-dir", "concurrency", "persist-interval", "stats", "trackers", "module",
}

var knownModuleKeys = []string{
	"peer-id-blacklist", "client-name-blacklist", "ip-address-blocker", "progress-cheat-blocker",
	"auto-range-ban", "multi-dialing-blocker", "expression-engine", "ip-address-blocker-rules",
	"active-monitoring",
}

func auditConfigKeys(raw map[string]any) error {
	for old, replacement := range renamedKeys {
		if _, ok := raw[old]; ok {
			return fmt.Errorf("配置键 %q 已更名为 %q，请更新配置文件", old, replacement)
		}
	}
	modules, _ := raw["module"].(map[string]any)
	for name, replacement := range renamedRuleKeys {
		section, _ := modules[name].(map[string]any)
		if _, ok := section["rules"]; ok {
			return fmt.Errorf("模块 %q 的规则键已更名为 %q，请更新配置文件", name, replacement)
		}
	}
	for key := range raw {
		if !slices.Contains(knownTopKeys, key) {
			logf("配置项 %q 不被识别，已忽略", key)
		}
	}
	for name := range modules {
		if !slices.Contains(knownModuleKeys, name) {
			logf("配置项 \"module.%s\" 不被识别，已忽略", name)
		}
	}
	return nil
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(false)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if err := auditConfigKeys(raw); err != nil {
		return nil, err
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = Duration(5 * time.Second)
	}
	if cfg.PersistInterval <= 0 {
		cfg.PersistInterval = Duration(30 * time.Second)
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}
	if cfg.Stats.SampleInterval <= 0 {
		cfg.Stats.SampleInterval = Duration(5 * time.Minute)
	}
	if cfg.Trackers.Sources == nil {
		cfg.Trackers.Sources = defaultTrackersConfig().Sources
	}
	if cfg.Trackers.RefreshInterval <= 0 {
		cfg.Trackers.RefreshInterval = Duration(24 * time.Hour)
	}
	if err := cfg.Trackers.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (m baseModule) duration(fallback time.Duration) time.Duration {
	if m.BanDuration > 0 {
		return time.Duration(m.BanDuration)
	}
	return fallback
}

type banManager struct {
	st      *State
	foreign map[string]struct{}
	dropped map[string]struct{}
	added   []string
	removed bool
}

func (b *banManager) add(p *Peer, t *Torrent, res *Result) bool {
	ip := p.Addr.String()
	until := time.Now().Add(res.Duration).UnixMilli()

	b.st.mu.Lock()
	defer b.st.mu.Unlock()
	if _, ok := b.foreign[ip]; ok {
		return false
	}
	if existing, ok := b.st.Bans[ip]; ok {
		if !(existing.Disconnect && res.Verdict == verdictBan) {
			if until > existing.UntilMs {
				existing.UntilMs = until
				b.st.dirty = true
			}
			return false
		}
		existing.Disconnect = false
		existing.Module = res.Module
		existing.Reason = res.Reason
		existing.Torrent = t.Name
		if until > existing.UntilMs {
			existing.UntilMs = until
		}
		b.added = append(b.added, p.Key)
		b.st.dirty = true
		return true
	}
	b.st.Bans[ip] = &BanRecord{
		IP:         ip,
		Module:     res.Module,
		Reason:     res.Reason,
		Torrent:    t.Name,
		UntilMs:    until,
		Disconnect: res.Verdict == verdictDisconnect,
	}
	b.added = append(b.added, p.Key)
	b.st.dirty = true
	return true
}

func (b *banManager) addManual(ip string, until time.Time, reason string) BanRecord {
	b.st.mu.Lock()
	defer b.st.mu.Unlock()
	rec := &BanRecord{IP: ip, Module: "manual", Reason: reason, UntilMs: until.UnixMilli()}
	b.st.Bans[ip] = rec
	delete(b.dropped, ip)
	b.removed = true
	b.st.dirty = true
	return *rec
}

func (b *banManager) remove(ip string) bool {
	b.st.mu.Lock()
	defer b.st.mu.Unlock()
	_, ours := b.st.Bans[ip]
	_, foreign := b.foreign[ip]
	if !ours && !foreign {
		return false
	}
	delete(b.st.Bans, ip)
	delete(b.foreign, ip)
	b.markDropped(ip)
	b.removed = true
	b.st.dirty = true
	return true
}

func (b *banManager) markDropped(ip string) {
	if b.dropped == nil {
		b.dropped = map[string]struct{}{}
	}
	b.dropped[ip] = struct{}{}
}

func (b *banManager) expire() []BanRecord {
	now := time.Now().UnixMilli()
	b.st.mu.Lock()
	defer b.st.mu.Unlock()
	var gone []BanRecord
	for ip, rec := range b.st.Bans {
		if rec.UntilMs > 0 && rec.UntilMs < now {
			gone = append(gone, *rec)
			delete(b.st.Bans, ip)
			b.markDropped(ip)
			b.removed = true
			b.st.dirty = true
		}
	}
	return gone
}

func (b *banManager) banned(addr netip.Addr) bool {
	b.st.mu.Lock()
	defer b.st.mu.Unlock()
	rec, ok := b.st.Bans[addr.String()]
	return ok && !rec.Disconnect
}

func (b *banManager) active() map[netip.Addr]bool {
	b.st.mu.Lock()
	defer b.st.mu.Unlock()
	out := make(map[netip.Addr]bool, len(b.st.Bans))
	for ip, rec := range b.st.Bans {
		if rec.Disconnect {
			continue
		}
		if addr, err := netip.ParseAddr(ip); err == nil {
			out[addr] = true
		}
	}
	return out
}

func (b *banManager) sync(qb *QBClient, shadow bool) error {
	b.st.mu.Lock()
	added := b.added
	removed := b.removed
	b.added = nil
	b.removed = false
	b.st.mu.Unlock()

	if !removed {
		if len(added) == 0 {
			return nil
		}
		return qb.BanPeers(added, shadow)
	}

	b.refreshForeign(qb, shadow)

	b.st.mu.Lock()
	list := make([]string, 0, len(b.st.Bans)+len(b.foreign))
	for _, rec := range b.st.Bans {
		list = append(list, rec.IP)
	}
	for ip := range b.foreign {
		list = append(list, ip)
	}
	b.st.mu.Unlock()

	logf("回写全量封禁列表：%d 条", len(list))
	if err := qb.SetBanList(list, shadow); err != nil {
		return err
	}
	b.st.mu.Lock()
	b.dropped = nil
	b.st.mu.Unlock()
	return nil
}

func (b *banManager) refreshForeign(qb *QBClient, shadow bool) {
	prefs, err := qb.Preferences()
	if err != nil {
		logf("读取下载器封禁列表失败，本轮按本地记录回写: %v", err)
		return
	}
	field := "banned_IPs"
	if shadow {
		field = "shadow_banned_IPs"
	}
	raw, _ := prefs[field].(string)

	b.st.mu.Lock()
	defer b.st.mu.Unlock()
	next := map[string]struct{}{}
	for _, line := range strings.Split(raw, "\n") {
		ip := strings.TrimSpace(line)
		if ip == "" {
			continue
		}
		if _, ours := b.st.Bans[ip]; ours {
			continue
		}
		if _, gone := b.dropped[ip]; gone {
			continue
		}
		next[ip] = struct{}{}
	}
	b.foreign = next
}

func (b *banManager) snapshot() []BanRecord {
	b.st.mu.Lock()
	defer b.st.mu.Unlock()
	out := make([]BanRecord, 0, len(b.st.Bans))
	for _, rec := range b.st.Bans {
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UntilMs > out[j].UntilMs })
	return out
}

type counters struct {
	cycles        int64
	peers         int64
	banned        int64
	skipped       int64
	alreadyBanned int64
	errors        int64
	torrents      int64
}

type runtime struct {
	modules []Module
	pcb     *pcbModule
	expr    *expressionModule
	subs    *ipRuleListModule
	monitor *activeMonitoringConfig
	ignored []netip.Prefix
}

type App struct {
	cfg  atomic.Pointer[Config]
	qb   *QBClient
	st   *State
	bans *banManager

	rt   atomic.Pointer[runtime]
	hub  *eventHub
	auth *authService

	trackers *trackerSet

	configPath string
	alertDay   string

	counters     counters
	stats        *statsStore
	lastCycleNs  atomic.Int64
	cycleTakenNs atomic.Int64
	startedAt    time.Time

	cfgMu sync.Mutex
}

var activeStates = map[string]bool{
	"uploading": true, "stalledUP": true, "forcedUP": true,
	"downloading": true, "stalledDL": true, "forcedDL": true,
}

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	st, err := loadState(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		log.Fatalf("状态加载失败: %v", err)
	}
	var app *App
	app = &App{
		st:         st,
		bans:       &banManager{st: st, foreign: map[string]struct{}{}},
		stats:      newStatsStore(cfg.DataDir, func() statsWindow { return app.cfg.Load().Stats.window() }),
		configPath: resolveConfigPath(configPath),
		startedAt:  time.Now(),
	}
	app.cfg.Store(cfg)

	qb, err := newQBClient(func() QBConfig { return app.cfg.Load().QB })
	if err != nil {
		log.Fatalf("初始化下载器客户端失败: %v", err)
	}
	app.qb = qb
	if err := qb.ensureLogin(); err != nil {
		logf("下载器登录失败（将在后续周期重试）: %v", err)
	} else {
		logf("已连接下载器 %s", cfg.QB.URL)
	}

	if err := app.stats.load(); err != nil {
		logf("读取流量历史失败: %v", err)
	}
	app.auth = newAuthService(filepath.Join(cfg.DataDir, "sessions.json"))
	app.hub = newEventHub()
	app.trackers = newTrackerSet(filepath.Join(cfg.DataDir, "trackers"), qb)
	app.trackers.notify = func() { app.hub.publish("trackers", app.trackers.snapshot()) }
	if err := app.applyRuntime(); err != nil {
		log.Fatalf("模块初始化失败: %v", err)
	}
	app.loadForeignBans()
	if cfg.Trackers.Enabled {
		app.trackers.Load(cfg.Trackers)
	}

	shadow := app.shadowMode()
	if shadow {
		if prefs, err := qb.Preferences(); err == nil {
			if enabled, _ := prefs["shadow_ban_enabled"].(bool); !enabled {
				logf("⚠ 配置为 shadowban 模式，但下载器未开启 shadow_ban_enabled，封禁不会生效")
			}
		}
		logf("⚠ shadowban 模式下 fast-pcb-test 不生效（影子封禁不断开连接，测不出真实速度），建议将 fast-pcb-test-percentage 设为 0")
	}

	stop := make(chan struct{})
	go st.flushLoop(func() time.Duration {
		return time.Duration(app.cfg.Load().PersistInterval)
	}, stop)

	go app.monitorLoop(stop)
	go app.subsLoop(stop)
	go app.trackersLoop(stop)
	go app.pruneLoop(stop)
	go app.statsLoop(stop)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		close(stop)
		if err := st.flush(); err != nil {
			logf("退出前落盘失败: %v", err)
		}
		if err := app.stats.save(); err != nil {
			logf("退出前写入流量历史失败: %v", err)
		}
		logf("已停止")
		os.Exit(0)
	}()

	app.serveHTTP()
	app.runLoop(stop)
}

func (a *App) shadowMode() bool {
	return strings.EqualFold(a.cfg.Load().QB.BanMethod, "shadowban")
}

func (a *App) buildRuntime() (*runtime, error) {
	global := time.Duration(a.cfg.Load().BanDuration)
	if global <= 0 {
		global = 14 * 24 * time.Hour
	}
	m := a.cfg.Load().Modules
	rt := &runtime{}

	for _, raw := range a.cfg.Load().IgnoreAddresses {
		prefix, err := parsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("ignore-peers-from-addresses 中的 %q 无法解析: %w", raw, err)
		}
		rt.ignored = append(rt.ignored, prefix)
	}

	if c := m.PeerIDBlacklist; c != nil && c.Enabled {
		rules, err := parseRules(c.BannedPeerID)
		if err != nil {
			return nil, fmt.Errorf("peer-id-blacklist: %w", err)
		}
		rt.modules = append(rt.modules, &ruleListModule{
			name: "peer-id-blacklist", dur: c.duration(global), rules: rules, label: "PeerID",
			get: func(p *Peer) string { return p.PeerID() },
		})
	}
	if c := m.ClientNameBlacklist; c != nil && c.Enabled {
		rules, err := parseRules(c.BannedClientName)
		if err != nil {
			return nil, fmt.Errorf("client-name-blacklist: %w", err)
		}
		rt.modules = append(rt.modules, &ruleListModule{
			name: "client-name-blacklist", dur: c.duration(global), rules: rules, label: "客户端名称",
			get: func(p *Peer) string { return p.Client },
		})
	}
	if c := m.IPBlacklist; c != nil && c.Enabled {
		mod := &ipBlacklistModule{dur: c.duration(global), ports: map[uint16]bool{}}
		for _, raw := range c.IPs {
			prefix, err := parsePrefix(raw)
			if err != nil {
				return nil, fmt.Errorf("ip-address-blocker.ips 中的 %q 无法解析: %w", raw, err)
			}
			mod.ips = append(mod.ips, prefix)
		}
		for _, port := range c.Ports {
			mod.ports[uint16(port)] = true
		}
		rt.modules = append(rt.modules, mod)
	}
	if c := m.PCB; c != nil && c.Enabled {
		rt.pcb = &pcbModule{
			minSize:    c.MinimumSize,
			blockExces: c.BlockExcessive,
			excessive:  c.Excessive,
			maxDiff:    c.MaxDifference,
			rewindMax:  c.RewindMaxDiff,
			v4Prefix:   prefixLen(c.IPv4PrefixLength, 32, 32),
			v6Prefix:   prefixLen(c.IPv6PrefixLength, 128, 56),
			dur:        c.duration(global),
			persist:    time.Duration(c.PersistDuration),
			maxWait:    time.Duration(c.MaxWaitDuration),
			fastPct:    c.FastTestPercent,
			fastDur:    time.Duration(c.FastTestBlockDur),
			st:         a.st,
		}
		if rt.pcb.persist <= 0 {
			rt.pcb.persist = 14 * 24 * time.Hour
		}
		rt.modules = append(rt.modules, rt.pcb)
	}
	if c := m.MultiDialing; c != nil && c.Enabled {
		rt.modules = append(rt.modules, &multiDialingModule{
			dur: c.duration(global), maskV4: prefixLen(c.SubnetMask, 32, 24),
			maskV6:     prefixLen(c.SubnetMaskV6, 128, 56),
			tolerateV4: c.TolerateV4, tolerateV6: c.TolerateV6,
			lifespan: time.Duration(c.CacheLifespan), keepHunting: c.KeepHunting,
			huntTime: time.Duration(c.KeepHuntingTime), st: a.st,
		})
	}
	if c := m.AutoRangeBan; c != nil && c.Enabled {
		rt.modules = append(rt.modules, &autoRangeBanModule{
			dur: c.duration(global), v4: prefixLen(c.IPv4, 32, 30),
			v6: prefixLen(c.IPv6, 128, 48), bans: a.bans.active,
		})
	}
	if c := m.Expression; c != nil && c.Enabled {
		rt.expr = &expressionModule{dur: c.duration(global), dir: filepath.Join(a.cfg.Load().DataDir, "scripts")}
		if err := rt.expr.Reload(); err != nil {
			return nil, fmt.Errorf("expression-engine: %w", err)
		}
		rt.modules = append(rt.modules, rt.expr)
	}
	if c := m.IPRuleList; c != nil && c.Enabled {
		rt.subs = &ipRuleListModule{
			dur: c.duration(global), subs: c.subscriptions(), dir: filepath.Join(a.cfg.Load().DataDir, "sub"),
			http: &http.Client{Timeout: 5 * time.Minute},
		}
		rt.modules = append(rt.modules, rt.subs)
	}
	if c := m.ActiveMonitoring; c != nil && c.Enabled {
		rt.monitor = c
	}
	return rt, nil
}

func (a *App) applyRuntime() error {
	rt, err := a.buildRuntime()
	if err != nil {
		return err
	}
	a.rt.Store(rt)
	logf("已启用 %d 个检测模块", len(rt.modules))
	if rt.subs != nil {
		go rt.subs.Refresh()
	}
	a.hub.publish("config", map[string]any{
		"reloaded":    a.moduleNames(),
		"needRestart": []string{},
	})
	return nil
}

func (a *App) moduleNames() []string {
	names := []string{}
	if rt := a.rt.Load(); rt != nil {
		for _, mod := range rt.modules {
			names = append(names, mod.Name())
		}
	}
	return names
}

func (a *App) loadForeignBans() {
	prefs, err := a.qb.Preferences()
	if err != nil {
		logf("读取下载器封禁列表失败: %v", err)
		return
	}
	field := "banned_IPs"
	if a.shadowMode() {
		field = "shadow_banned_IPs"
	}
	raw, _ := prefs[field].(string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ours := a.st.Bans[line]; ours {
			continue
		}
		a.bans.foreign[line] = struct{}{}
	}
	logf("下载器现有封禁 %d 条（本服务 %d 条，外部 %d 条，外部条目在全量回写时保留）",
		len(a.st.Bans)+len(a.bans.foreign), len(a.st.Bans), len(a.bans.foreign))
}

func (a *App) ignoredAddr(addr netip.Addr) bool {
	rt := a.rt.Load()
	if rt == nil {
		return false
	}
	for _, prefix := range rt.ignored {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (a *App) runLoop(stop <-chan struct{}) {
	current := time.Duration(a.cfg.Load().CheckInterval)
	ticker := time.NewTicker(current)
	defer ticker.Stop()
	a.cycle()
	for {
		select {
		case <-ticker.C:
			if next := time.Duration(a.cfg.Load().CheckInterval); next != current {
				current = next
				ticker.Reset(current)
			}
			a.cycle()
		case <-stop:
			return
		}
	}
}

func (a *App) cycle() {
	start := time.Now()
	torrents, err := a.qb.Torrents()
	if err != nil {
		atomic.AddInt64(&a.counters.errors, 1)
		logf("获取任务列表失败: %v", err)
		return
	}
	active := make([]*Torrent, 0, len(torrents))
	for i := range torrents {
		if activeStates[torrents[i].State] {
			active = append(active, &torrents[i])
		}
	}
	atomic.StoreInt64(&a.counters.torrents, int64(len(active)))

	sem := make(chan struct{}, a.cfg.Load().Concurrency)
	var wg sync.WaitGroup
	for _, t := range active {
		wg.Add(1)
		sem <- struct{}{}
		go func(t *Torrent) {
			defer wg.Done()
			defer func() { <-sem }()
			a.checkTorrent(t)
		}(t)
	}
	wg.Wait()

	for _, rec := range a.bans.expire() {
		a.hub.publish("ban", map[string]any{"action": "remove", "record": rec, "reason": "已到期"})
	}
	if err := a.bans.sync(a.qb, a.shadowMode()); err != nil {
		atomic.AddInt64(&a.counters.errors, 1)
		logf("应用封禁失败: %v", err)
		a.hub.publish("error", map[string]string{"message": "应用封禁失败: " + err.Error()})
	}
	atomic.AddInt64(&a.counters.cycles, 1)
	a.lastCycleNs.Store(time.Now().UnixNano())
	a.cycleTakenNs.Store(int64(time.Since(start)))
	a.hub.publish("status", a.statusPayload())
}

func (a *App) checkTorrent(t *Torrent) {
	peers, err := a.qb.Peers(t.ID)
	if err != nil {
		atomic.AddInt64(&a.counters.errors, 1)
		logf("获取任务 %q 的 Peer 列表失败: %v", t.Name, err)
		return
	}
	atomic.AddInt64(&a.counters.peers, int64(len(peers)))
	for i := range peers {
		a.checkPeer(t, &peers[i])
	}
}

func (a *App) evaluate(t *Torrent, p *Peer) (best *Result, all []*Result) {
	rt := a.rt.Load()
	if rt == nil {
		return nil, nil
	}
	for _, mod := range rt.modules {
		res := mod.Check(t, p)
		if res == nil {
			continue
		}
		all = append(all, res)
		if res.Verdict == verdictSkip {
			break
		}
		if best == nil || res.Verdict > best.Verdict ||
			(res.Verdict == best.Verdict && res.Duration > best.Duration) {
			best = res
		}
	}
	return best, all
}

func (a *App) checkPeer(t *Torrent, p *Peer) {
	if a.ignoredAddr(p.Addr) {
		atomic.AddInt64(&a.counters.skipped, 1)
		return
	}
	if a.bans.banned(p.Addr) {
		atomic.AddInt64(&a.counters.alreadyBanned, 1)
		return
	}
	best, _ := a.evaluate(t, p)
	if best == nil {
		return
	}
	if a.bans.add(p, t, best) {
		atomic.AddInt64(&a.counters.banned, 1)
		logf("封禁 %s (%s)：%s — %s", p.Addr, p.Client, best.Module, best.Reason)
		a.hub.publish("ban", map[string]any{"action": "add", "ip": p.Addr.String(),
			"record": a.bans.record(p.Addr.String())})
	}
}

func (b *banManager) record(ip string) *BanRecord {
	b.st.mu.Lock()
	defer b.st.mu.Unlock()
	if rec, ok := b.st.Bans[ip]; ok {
		cp := *rec
		return &cp
	}
	return nil
}

func (a *App) monitorLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.monitorTick()
		case <-stop:
			return
		}
	}
}

func (a *App) monitorTick() {
	rt := a.rt.Load()
	if rt == nil || rt.monitor == nil {
		return
	}
	a.st.mu.Lock()
	rid, lastUL, lastDL := a.st.RID, a.st.LastUL, a.st.LastDL
	a.st.mu.Unlock()

	data, err := a.qb.MainData(rid)
	if err != nil {
		return
	}
	ul, dl := data.ServerState.AlltimeUL, data.ServerState.AlltimeDL

	var day string
	var today int64
	a.st.mu.Lock()
	a.st.RID = data.RID
	if lastUL > 0 && ul >= lastUL {
		day = time.Now().Format("2006-01-02")
		entry := a.st.Traffic[day]
		if entry == nil {
			entry = &TrafficDay{}
			a.st.Traffic[day] = entry
		}
		entry.Uploaded += ul - lastUL
		entry.Downloaded += dl - lastDL
		a.st.dirty = true
		today = entry.Uploaded
	}
	a.st.LastUL, a.st.LastDL = ul, dl
	a.st.mu.Unlock()

	cap := rt.monitor.Traffic.Daily
	if cap > 0 && day != "" && today >= cap && a.alertDay != day {
		a.alertDay = day
		logf("⚠ 今日上传流量 %s 已达阈值 %s", humanBytes(today), humanBytes(cap))
	}
}

func (a *App) statsLoop(stop <-chan struct{}) {
	a.statsSample()
	current := time.Duration(a.cfg.Load().Stats.SampleInterval)
	ticker := time.NewTicker(current)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if next := time.Duration(a.cfg.Load().Stats.SampleInterval); next != current {
				current = next
				ticker.Reset(current)
			}
			a.statsSample()
		case <-stop:
			return
		}
	}
}

func (a *App) statsSample() {
	torrents, err := a.qb.Torrents()
	if err != nil {
		logf("流量采样失败: %v", err)
		return
	}
	data, err := a.qb.MainData(0)
	if err != nil {
		logf("流量采样失败: %v", err)
		return
	}
	a.stats.sample(torrents, *data)
	if err := a.stats.save(); err != nil {
		logf("写入流量历史失败: %v", err)
	}
	a.hub.publish("stats", a.stats.payload())
}

func (a *App) subsInterval() time.Duration {
	if c := a.cfg.Load().Modules.IPRuleList; c != nil && c.CheckInterval > 0 {
		return time.Duration(c.CheckInterval)
	}
	return time.Hour
}

func (a *App) subsLoop(stop <-chan struct{}) {
	current := a.subsInterval()
	ticker := time.NewTicker(current)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if next := a.subsInterval(); next != current {
				current = next
				ticker.Reset(current)
			}
			if rt := a.rt.Load(); rt != nil && rt.subs != nil {
				rt.subs.Refresh()
			}
		case <-stop:
			return
		}
	}
}

func (a *App) pruneLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(8 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rt := a.rt.Load()
			a.st.mu.Lock()
			if rt != nil && rt.pcb != nil {
				if n := a.st.prune(rt.pcb.persist); n > 0 {
					logf("清理过期检测记录 %d 条", n)
				}
			}
			cutoff := time.Now().AddDate(0, 0, -400).Format("2006-01-02")
			for day := range a.st.Traffic {
				if day < cutoff {
					delete(a.st.Traffic, day)
					a.st.dirty = true
				}
			}
			a.st.mu.Unlock()
		case <-stop:
			return
		}
	}
}

func humanBytes(v int64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d B", v)
	}
	div, exp := int64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(v)/float64(div), "KMGTPE"[exp])
}

func prefixLen(v, bits, def int) int {
	if v < 1 || v > bits {
		return def
	}
	return v
}

type statusPayload struct {
	Version        string         `json:"version"`
	Uptime         string         `json:"uptime"`
	QBURL          string         `json:"qbUrl"`
	BanMethod      string         `json:"banMethod"`
	CheckEvery     string         `json:"checkInterval"`
	Cycles         int64          `json:"cycles"`
	ActiveTorrents int64          `json:"activeTorrents"`
	PeersChecked   int64          `json:"peersChecked"`
	BansIssued     int64          `json:"bansIssued"`
	Skipped        int64          `json:"skipped"`
	AlreadyBanned  int64          `json:"alreadyBanned"`
	Errors         int64          `json:"errors"`
	LastCycle      string         `json:"lastCycle"`
	CycleTakenMs   int64          `json:"cycleTakenMs"`
	CurrentBans    int            `json:"currentBans"`
	Modules        []string       `json:"modules"`
	Subscriptions  map[string]int `json:"subscriptions"`
	TrafficToday   *TrafficDay    `json:"trafficToday"`
	Bans           []BanRecord    `json:"bans"`
}
