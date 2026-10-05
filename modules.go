
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

type verdict int

const (
	verdictNone verdict = iota
	verdictDisconnect
	verdictBan
	verdictSkip
)

type Result struct {
	Verdict  verdict
	Module   string
	Reason   string
	Duration time.Duration
	Detail   map[string]any
}

type Module interface {
	Name() string
	Check(t *Torrent, p *Peer) *Result
}

func ban(module string, dur time.Duration, reason string, detail map[string]any) *Result {
	return &Result{Verdict: verdictBan, Module: module, Duration: dur, Reason: reason, Detail: detail}
}

func disconnect(module string, dur time.Duration, reason string, detail map[string]any) *Result {
	return &Result{Verdict: verdictDisconnect, Module: module, Duration: dur, Reason: reason, Detail: detail}
}

func skip(module, reason string) *Result {
	return &Result{Verdict: verdictSkip, Module: module, Reason: reason}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func pct(v float64) string { return strconv.FormatFloat(v*100, 'f', 2, 64) + "%" }

func parsePrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

type ruleListModule struct {
	name  string
	dur   time.Duration
	rules []Rule
	get   func(*Peer) string
	label string
}

func (m *ruleListModule) Name() string { return m.name }

func (m *ruleListModule) Check(_ *Torrent, p *Peer) *Result {
	value := m.get(p)
	if value == "" {
		return nil
	}
	hit, rule := matchRules(m.rules, value)
	if !hit {
		return nil
	}
	return ban(m.name, m.dur, fmt.Sprintf("%s %q 命中规则 %s", m.label, value, rule.describe()),
		map[string]any{"field": m.label, "value": value, "rule": rule.describe()})
}

type ipBlacklistModule struct {
	dur   time.Duration
	ips   []netip.Prefix
	ports map[uint16]bool
}

func (m *ipBlacklistModule) Name() string { return "ip-address-blocker" }

func (m *ipBlacklistModule) Check(_ *Torrent, p *Peer) *Result {
	if p.Handshaking() {
		return nil
	}
	if m.ports[uint16(p.Port)] {
		return ban(m.Name(), m.dur, fmt.Sprintf("端口 %d 命中黑名单", p.Port), map[string]any{"type": "port", "port": p.Port})
	}
	for _, cidr := range m.ips {
		if cidr.Contains(p.Addr) {
			return ban(m.Name(), m.dur, fmt.Sprintf("IP 命中黑名单网段 %s", cidr), map[string]any{"type": "ip", "cidr": cidr.String()})
		}
	}
	return nil
}

type pcbModule struct {
	minSize    int64
	blockExces bool
	excessive  float64
	maxDiff    float64
	rewindMax  float64
	v4Prefix   int
	v6Prefix   int
	dur        time.Duration
	persist    time.Duration
	maxWait    time.Duration
	fastPct    float64
	fastDur    time.Duration
	st         *State
}

func (m *pcbModule) Name() string { return "progress-cheat-blocker" }

func (m *pcbModule) tooSmall(size int64) bool { return size < m.minSize }

func (m *pcbModule) Check(t *Torrent, p *Peer) *Result {
	if p.Handshaking() {
		return nil
	}
	prefixLen := m.v4Prefix
	if p.Addr.Is6() {
		prefixLen = m.v6Prefix
	}
	prefix := netip.PrefixFrom(p.Addr, prefixLen).Masked().String()

	m.st.mu.Lock()
	defer m.st.mu.Unlock()

	rk := t.ID + "|" + prefix
	ak := t.ID + "|" + p.Addr.String() + "|" + strconv.Itoa(p.Port)
	r := m.st.Ranges[rk]
	if r == nil {
		r = &pcbRange{TorrentID: t.ID, Prefix: prefix}
		m.st.Ranges[rk] = r
	}
	a := m.st.Addrs[ak]
	if a == nil {
		a = &pcbAddr{TorrentID: t.ID, IP: p.Addr.String(), Port: p.Port}
		m.st.Addrs[ak] = a
	}

	var incr int64
	if p.Uploaded >= 0 {
		if p.Uploaded < a.LastReportUploaded {
			incr = p.Uploaded
		} else {
			incr = p.Uploaded - a.LastReportUploaded
		}
		a.TrackingUploaded += incr
		r.TrackingUploaded += incr
	}
	computed := max64(p.Uploaded, max64(a.TrackingUploaded, r.TrackingUploaded))

	defer m.commit(t, p, r, a)

	if t.Size <= 0 || !p.UploadingToPeer() {
		return nil
	}
	if res := m.fastTest(r, a, computed, t.Size); res != nil {
		return res
	}
	computedProgress := float64(computed) / float64(t.Size)
	reported := p.Progress
	if res := m.excessiveTest(r, a, computed, t.Size, t.Completed); res != nil {
		return res
	}
	if computedProgress <= reported {
		return nil
	}
	if res := m.differenceTest(r, a, p, reported, computedProgress, t.Size); res != nil {
		return res
	}
	return m.rewindTest(r, a, p, reported, computedProgress, t.Size)
}

func (m *pcbModule) commit(t *Torrent, p *Peer, r *pcbRange, a *pcbAddr) {
	now := time.Now().UnixMilli()
	a.LastReportUploaded = p.Uploaded
	if p.Progress != 0 {
		a.LastReportProgress = p.Progress
		r.LastReportProgress = p.Progress
	}
	a.LastTorrentCompletedSize = max64(t.Completed, a.LastTorrentCompletedSize)
	a.LastSeen = now
	r.LastReportUploaded = p.Uploaded
	r.LastTorrentCompletedSize = max64(t.Completed, r.LastTorrentCompletedSize)
	r.LastSeen = now
	m.st.dirty = true
}

func (m *pcbModule) fastTest(r *pcbRange, a *pcbAddr, computed, torrentSize int64) *Result {
	if m.fastPct <= 0 || m.tooSmall(torrentSize) {
		return nil
	}
	if a.FastPcbTestExecuteAt != 0 && r.FastPcbTestExecuteAt != 0 {
		return nil
	}
	if float64(computed) < m.fastPct*float64(torrentSize) {
		return nil
	}
	now := time.Now().UnixMilli()
	a.FastPcbTestExecuteAt = now
	r.FastPcbTestExecuteAt = now
	return disconnect(m.Name(), m.fastDur, "快速进度测试：主动断开连接以预热进度重置检查",
		map[string]any{"type": "fastPcbTest", "actualUploaded": computed, "torrentSize": torrentSize})
}

func (m *pcbModule) excessiveTest(r *pcbRange, a *pcbAddr, computed, torrentSize, completedSize int64) *Result {
	if !m.blockExces {
		return nil
	}
	if computed > torrentSize {
		limit := int64(float64(max64(torrentSize, m.minSize)) * m.excessive)
		if computed > limit {
			m.resetWindow(r, a)
			return ban(m.Name(), m.dur,
				fmt.Sprintf("过量下载：累计上传 %d 字节，超过种子大小 %d × 阈值 %.2f = %d", computed, torrentSize, m.excessive, limit),
				map[string]any{"type": "excessiveMaxDownloadThreshold", "actualUploaded": computed, "limit": limit})
		}
		return nil
	}
	if completedSize <= 0 || computed <= completedSize {
		return nil
	}
	computedCompleted := max64(completedSize, max64(r.LastTorrentCompletedSize, a.LastTorrentCompletedSize))
	limit := int64(float64(max64(computedCompleted, m.minSize)) * m.excessive)
	if computed > limit {
		m.resetWindow(r, a)
		return ban(m.Name(), m.dur,
			fmt.Sprintf("过量下载：任务仅完成 %d 字节，对方已累计下载 %d 字节（阈值 %d）", completedSize, computed, limit),
			map[string]any{"type": "excessiveMaxDownloadThresholdForIncompleteTask", "actualUploaded": computed, "limit": limit})
	}
	return nil
}

func (m *pcbModule) differenceTest(r *pcbRange, a *pcbAddr, p *Peer, reported, computed float64, torrentSize int64) *Result {
	diff := math.Abs(computed - reported)
	if diff <= m.maxDiff || m.tooSmall(torrentSize) || !p.UploadingToPeer() {
		return nil
	}
	if !m.windowScheduled(r, a) {
		m.scheduleWindow(r, a)
		return nil
	}
	if !m.windowExpired(r, a) {
		return nil
	}
	r.ProgressDifferenceCounter++
	a.ProgressDifferenceCounter++
	m.resetWindow(r, a)
	return ban(m.Name(), m.dur,
		fmt.Sprintf("汇报进度 %s 低于实际进度 %s，差值 %s 超过阈值 %s", pct(reported), pct(computed), pct(diff), pct(m.maxDiff)),
		map[string]any{"type": "deSyncDifference", "reported": reported, "computed": computed, "difference": diff,
			"peerCounter": a.ProgressDifferenceCounter, "prefixCounter": r.ProgressDifferenceCounter})
}

func (m *pcbModule) rewindTest(r *pcbRange, a *pcbAddr, p *Peer, reported, computed float64, torrentSize int64) *Result {
	if m.rewindMax <= 0 || m.tooSmall(torrentSize) {
		return nil
	}
	last := math.Max(a.LastReportProgress, r.LastReportProgress)
	rewind := last - p.Progress
	if rewind <= m.rewindMax || !p.UploadingToPeer() {
		return nil
	}
	if p.Progress > 0 || m.windowExpired(r, a) {
		a.RewindCounter++
		r.RewindCounter++
		m.resetWindow(r, a)
		return ban(m.Name(), m.dur,
			fmt.Sprintf("进度倒退：历史进度 %s 现报 %s，回退 %s 超过阈值 %s", pct(last), pct(p.Progress), pct(rewind), pct(m.rewindMax)),
			map[string]any{"type": "rewindProgress", "lastReportProgress": last, "rewind": rewind,
				"reported": reported, "computed": computed})
	}
	if !m.windowScheduled(r, a) {
		m.scheduleWindow(r, a)
	}
	return nil
}

func (m *pcbModule) scheduleWindow(r *pcbRange, a *pcbAddr) {
	end := time.Now().Add(m.maxWait).UnixMilli()
	if r.BanDelayWindowEndAt <= 0 {
		r.BanDelayWindowEndAt = end
	}
	if a.BanDelayWindowEndAt <= 0 {
		a.BanDelayWindowEndAt = end
	}
}

func (m *pcbModule) windowScheduled(r *pcbRange, a *pcbAddr) bool {
	return r.BanDelayWindowEndAt > 0 || a.BanDelayWindowEndAt > 0
}

func (m *pcbModule) windowExpired(r *pcbRange, a *pcbAddr) bool {
	now := time.Now().UnixMilli()
	return (r.BanDelayWindowEndAt > 0 && r.BanDelayWindowEndAt < now) ||
		(a.BanDelayWindowEndAt > 0 && a.BanDelayWindowEndAt < now)
}

func (m *pcbModule) resetWindow(r *pcbRange, a *pcbAddr) {
	if r.BanDelayWindowEndAt > 0 {
		r.BanDelayWindowEndAt = 0
	}
	if a.BanDelayWindowEndAt > 0 {
		a.BanDelayWindowEndAt = 0
	}
}

type autoRangeBanModule struct {
	dur  time.Duration
	v4   int
	v6   int
	bans func() map[netip.Addr]bool
}

func (m *autoRangeBanModule) Name() string { return "auto-range-ban" }

func (m *autoRangeBanModule) Check(_ *Torrent, p *Peer) *Result {
	if p.Handshaking() {
		return nil
	}
	active := m.bans()
	if active[p.Addr] {
		return nil
	}
	for banned := range active {
		if banned.Is4() != p.Addr.Is4() {
			continue
		}
		bits := m.v4
		if banned.Is6() {
			bits = m.v6
		}
		block := netip.PrefixFrom(banned, bits).Masked()
		if block.Contains(p.Addr) {
			return ban(m.Name(), m.dur,
				fmt.Sprintf("与已封禁 IP %s 同处 %s 网段", banned, block),
				map[string]any{"relatedBannedAddress": banned.String(), "cidr": block.String()})
		}
	}
	return nil
}

type multiDialingModule struct {
	dur         time.Duration
	maskV4      int
	maskV6      int
	tolerateV4  int
	tolerateV6  int
	lifespan    time.Duration
	keepHunting bool
	huntTime    time.Duration
	st          *State
}

func (m *multiDialingModule) Name() string { return "multi-dialing-blocker" }

func (m *multiDialingModule) Check(t *Torrent, p *Peer) *Result {
	if p.Handshaking() {
		return nil
	}
	bits := m.maskV4
	tolerate := m.tolerateV4
	if p.Addr.Is6() {
		bits = m.maskV6
		tolerate = m.tolerateV6
	}
	subnet := netip.PrefixFrom(p.Addr, bits).Masked()
	now := time.Now().UnixMilli()

	m.st.mu.Lock()
	defer m.st.mu.Unlock()

	key := t.ID + "@" + subnet.String()
	group := m.st.Subnets[key]
	if group == nil {
		group = map[string]int64{}
		m.st.Subnets[key] = group
	}
	for ip, seen := range group {
		if now-seen > m.lifespan.Milliseconds() {
			delete(group, ip)
		}
	}
	group[p.Addr.String()] = now
	m.st.dirty = true

	if len(group) > tolerate {
		m.st.Hunting[key] = now
		return ban(m.Name(), m.dur,
			fmt.Sprintf("网段 %s 内 %d 个 IP 同时下载同一任务（上限 %d）", subnet, len(group), tolerate),
			map[string]any{"subnet": subnet.String(), "count": len(group), "tolerate": tolerate})
	}
	if !m.keepHunting {
		return nil
	}
	seen, ok := m.st.Hunting[key]
	if !ok {
		return nil
	}
	if now-seen < m.huntTime.Milliseconds() {
		m.st.Hunting[key] = now
		return ban(m.Name(), m.dur,
			fmt.Sprintf("追猎命中：网段 %s 近期被判定为多拨", subnet),
			map[string]any{"subnet": subnet.String(), "hunting": true})
	}
	delete(m.st.Hunting, key)
	return nil
}

type exprScript struct {
	name string
	prog *vm.Program
}

type expressionModule struct {
	dur     time.Duration
	dir     string
	mu      sync.RWMutex
	scripts []exprScript
}

func (m *expressionModule) Name() string { return "expression-engine" }

func (m *expressionModule) Reload() error {
	entries, err := os.ReadDir(m.dir)
	if os.IsNotExist(err) {
		m.mu.Lock()
		m.scripts = nil
		m.mu.Unlock()
		return nil
	}
	if err != nil {
		return err
	}
	scripts := make([]exprScript, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".expr") {
			continue
		}
		path := filepath.Join(m.dir, entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		prog, err := expr.Compile(string(source), expr.AllowUndefinedVariables())
		if err != nil {
			return fmt.Errorf("脚本 %s 编译失败: %w", entry.Name(), err)
		}
		scripts = append(scripts, exprScript{name: entry.Name(), prog: prog})
	}
	m.mu.Lock()
	m.scripts = scripts
	m.mu.Unlock()
	return nil
}

func (m *expressionModule) Check(t *Torrent, p *Peer) *Result {
	m.mu.RLock()
	scripts := m.scripts
	m.mu.RUnlock()
	if len(scripts) == 0 {
		return nil
	}
	env := map[string]any{
		"peer":    peerEnv(p),
		"torrent": torrentEnv(t),
	}
	for _, s := range scripts {
		out, err := expr.Run(s.prog, env)
		if err != nil {
			logf("脚本 %s 执行失败: %v", s.name, err)
			continue
		}
		switch v := out.(type) {
		case bool:
			if v {
				return ban(m.Name(), m.dur, "脚本 "+s.name+" 返回 true", map[string]any{"script": s.name})
			}
		case int:
			if res := m.fromCode(s.name, v); res != nil {
				return res
			}
		case int64:
			if res := m.fromCode(s.name, int(v)); res != nil {
				return res
			}
		case float64:
			if res := m.fromCode(s.name, int(v)); res != nil {
				return res
			}
		case string:
			switch strings.ToLower(v) {
			case "ban", "true", "1":
				return ban(m.Name(), m.dur, "脚本 "+s.name+" 返回 "+v, map[string]any{"script": s.name})
			case "skip", "2":
				return skip(m.Name(), "脚本 "+s.name+" 要求跳过其余检查")
			}
		}
	}
	return nil
}

func (m *expressionModule) fromCode(name string, code int) *Result {
	switch code {
	case 1:
		return ban(m.Name(), m.dur, "脚本 "+name+" 返回 1", map[string]any{"script": name})
	case 2:
		return skip(m.Name(), "脚本 "+name+" 要求跳过其余检查")
	}
	return nil
}

func peerEnv(p *Peer) map[string]any {
	return map[string]any{
		"ip":            p.Addr.String(),
		"port":          p.Port,
		"peerId":        p.PeerID(),
		"clientName":    p.Client,
		"progress":      p.Progress,
		"uploadSpeed":   p.UpSpeed,
		"downloadSpeed": p.DlSpeed,
		"uploaded":      p.Uploaded,
		"downloaded":    p.Downloaded,
		"flags":         p.Flags,
		"connection":    p.Connection,
	}
}

func torrentEnv(t *Torrent) map[string]any {
	return map[string]any{
		"id":            t.ID,
		"hash":          t.ID,
		"name":          t.Name,
		"size":          t.Size,
		"completedSize": t.Completed,
		"progress":      t.Progress,
		"uploadSpeed":   t.UpSpeed,
		"downloadSpeed": t.DlSpeed,
		"isPrivate":     t.Private,
		"category":      t.Category,
		"tags":          t.Tags,
	}
}

type ipRuleSub struct {
	ID      string
	Name    string `yaml:"name"`
	URL     string `yaml:"url"`
	Enabled bool   `yaml:"enabled"`
}

type ipRuleListModule struct {
	dur  time.Duration
	subs []ipRuleSub
	dir  string
	http *http.Client
	mu   sync.RWMutex
	sets map[string][]netip.Prefix
}

func (m *ipRuleListModule) Name() string { return "ip-address-blocker-rules" }

func (m *ipRuleListModule) Check(_ *Torrent, p *Peer) *Result {
	if p.Handshaking() {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, set := range m.sets {
		for _, cidr := range set {
			if cidr.Contains(p.Addr) {
				return ban(m.Name(), m.dur,
					fmt.Sprintf("IP 命中订阅规则 %s 的 %s", id, cidr),
					map[string]any{"rule": id, "cidr": cidr.String()})
			}
		}
	}
	return nil
}

func (m *ipRuleListModule) Refresh() {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		logf("创建订阅目录失败: %v", err)
		return
	}
	for _, sub := range m.subs {
		if !sub.Enabled || sub.URL == "" {
			m.drop(sub.ID)
			continue
		}
		path := filepath.Join(m.dir, sub.ID+".txt")
		data, err := m.fetch(sub.URL)
		if err != nil {
			if _, statErr := os.Stat(path); statErr == nil {
				logf("订阅 %s 更新失败（沿用本地缓存）: %v", sub.ID, err)
				m.loadFromFile(sub.ID, path)
				continue
			}
			logf("订阅 %s 更新失败且无本地缓存: %v", sub.ID, err)
			continue
		}
		if cached, readErr := os.ReadFile(path); readErr == nil && sha256Hex(cached) == sha256Hex(data) {
			m.loadFromFile(sub.ID, path)
			continue
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			logf("订阅 %s 写入缓存失败: %v", sub.ID, err)
			continue
		}
		set := parseIPSet(string(data))
		m.store(sub.ID, set)
		logf("订阅 %s 已更新：%d 条网段", sub.ID, len(set))
	}
}

func (m *ipRuleListModule) fetch(url string) ([]byte, error) {
	resp, err := m.http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

func (m *ipRuleListModule) loadFromFile(id, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	m.store(id, parseIPSet(string(data)))
}

func (m *ipRuleListModule) store(id string, set []netip.Prefix) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sets == nil {
		m.sets = map[string][]netip.Prefix{}
	}
	m.sets[id] = set
}

func (m *ipRuleListModule) drop(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sets, id)
}

func (m *ipRuleListModule) Size() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]int, len(m.sets))
	for id, set := range m.sets {
		out[id] = len(set)
	}
	return out
}

func (m *ipRuleListModule) Count(id string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sets[id])
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func parseIPSet(data string) []netip.Prefix {
	var out []netip.Prefix
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.Contains(line, ",") {
			fields := strings.Split(line, ",")
			if len(fields) < 3 {
				continue
			}
			start, err1 := parsePrefix(strings.TrimSpace(fields[0]))
			end, err2 := parsePrefix(strings.TrimSpace(fields[1]))
			level, err3 := strconv.Atoi(strings.TrimSpace(fields[2]))
			if err1 != nil || err2 != nil || err3 != nil || level >= 128 {
				continue
			}
			if prefix, ok := spanPrefix(start.Addr(), end.Addr()); ok {
				out = append(out, prefix)
			}
			continue
		}
		if idx := commentIndex(line); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
			if line == "" {
				continue
			}
		}
		if prefix, err := parsePrefix(line); err == nil {
			out = append(out, prefix)
		}
	}
	return out
}

func commentIndex(line string) int {
	hash := strings.Index(line, "#")
	slash := strings.Index(line, "//")
	switch {
	case hash < 0:
		return slash
	case slash < 0:
		return hash
	case hash < slash:
		return hash
	default:
		return slash
	}
}

func spanPrefix(start, end netip.Addr) (netip.Prefix, bool) {
	if start.Is4() != end.Is4() {
		return netip.Prefix{}, false
	}
	var s, e []byte
	if start.Is4() {
		a, b := start.As4(), end.As4()
		s, e = a[:], b[:]
	} else {
		a, b := start.As16(), end.As16()
		s, e = a[:], b[:]
	}
	common := 0
	for i := range s {
		xor := s[i] ^ e[i]
		if xor == 0 {
			common += 8
			continue
		}
		for bit := 7; bit >= 0; bit-- {
			if xor&(1<<uint(bit)) != 0 {
				break
			}
			common++
		}
		break
	}
	if common == 0 {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(start, common).Masked(), true
}
