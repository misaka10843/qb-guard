package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const sessionCookie = "qbguard_session"

const sessionTTL = 30 * 24 * time.Hour

const sessionRenewAfter = 24 * time.Hour

func constantEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

type authService struct {
	mu       sync.Mutex
	sessions map[string]time.Time
	ttl      time.Duration
	limiter  *failLimiter
	path     string
}

func newAuthService(path string) *authService {
	s := &authService{
		sessions: map[string]time.Time{},
		ttl:      sessionTTL,
		limiter:  newFailLimiter(10, 15*time.Minute),
		path:     path,
	}
	s.load()
	return s
}

func (s *authService) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var raw map[string]int64
	if err := json.Unmarshal(data, &raw); err != nil {
		logf("会话文件无法解析，本次启动按未登录处理: %v", err)
		return
	}
	now := time.Now()
	for token, ms := range raw {
		if exp := time.UnixMilli(ms); exp.After(now) {
			s.sessions[token] = exp
		}
	}
	if n := len(s.sessions); n > 0 {
		logf("恢复了 %d 个未过期会话", n)
	}
}

func (s *authService) persistLocked() {
	if s.path == "" {
		return
	}
	raw := make(map[string]int64, len(s.sessions))
	for token, exp := range s.sessions {
		raw[token] = exp.UnixMilli()
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return
	}
	if err := writeAtomic(s.path, data); err != nil {
		logf("会话落盘失败: %v", err)
	}
}

func (s *authService) issue() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf)
	now := time.Now()
	s.mu.Lock()
	for t, exp := range s.sessions {
		if now.After(exp) {
			delete(s.sessions, t)
		}
	}
	s.sessions[token] = now.Add(s.ttl)
	s.persistLocked()
	s.mu.Unlock()
	return token
}

func (s *authService) drop(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.persistLocked()
	s.mu.Unlock()
}

func (s *authService) authorized(w http.ResponseWriter, r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return false
	}
	s.mu.Lock()
	exp, ok := s.sessions[c.Value]
	if !ok {
		s.mu.Unlock()
		return false
	}
	now := time.Now()
	if now.After(exp) {
		delete(s.sessions, c.Value)
		s.persistLocked()
		s.mu.Unlock()
		return false
	}
	renew := exp.Sub(now) < s.ttl-sessionRenewAfter
	s.sessions[c.Value] = now.Add(s.ttl)
	if renew {
		s.persistLocked()
	}
	s.mu.Unlock()
	if renew {
		setSessionCookie(w, c.Value, s.ttl)
	}
	return true
}

func setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl / time.Second),
	})
}

type failLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
}

func newFailLimiter(limit int, window time.Duration) *failLimiter {
	return &failLimiter{limit: limit, window: window, hits: map[string][]time.Time{}}
}

func (f *failLimiter) key(addr netip.Addr) string {
	bits := 50
	if addr.Is4() {
		bits = 24
	}
	prefix, err := addr.Prefix(bits)
	if err != nil {
		return addr.String()
	}
	return prefix.Masked().String()
}

func (f *failLimiter) blocked(addr netip.Addr) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.key(addr)
	now := time.Now()
	kept := make([]time.Time, 0, len(f.hits[k]))
	for _, t := range f.hits[k] {
		if now.Sub(t) < f.window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(f.hits, k)
	} else {
		f.hits[k] = kept
	}
	return len(kept) >= f.limit
}

func (f *failLimiter) fail(addr netip.Addr) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.key(addr)
	f.hits[k] = append(f.hits[k], time.Now())
}

func (f *failLimiter) reset(addr netip.Addr) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.hits, f.key(addr))
}

var scannerAgents = []string{"censys", "shodan", "zoomeye", "threatbook", "fofa", "zmap", "nmap", "archive"}

func isScanner(r *http.Request) bool {
	ua := strings.ToLower(r.UserAgent())
	for _, s := range scannerAgents {
		if strings.Contains(ua, s) {
			return true
		}
	}
	return false
}

func fakeNginx404(w http.ResponseWriter) {
	w.Header().Set("Server", "nginx")
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("<html>\r\n<head><title>404 Not Found</title></head>\r\n" +
		"<body>\r\n<center><h1>404 Not Found</h1></center>\r\n" +
		"<hr><center>nginx</center>\r\n</body>\r\n</html>\r\n"))
}

func clientAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

type eventMsg struct {
	name string
	data any
}

type eventHub struct {
	mu      sync.Mutex
	clients map[chan eventMsg]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{clients: map[chan eventMsg]struct{}{}}
}

func (h *eventHub) subscribe() chan eventMsg {
	ch := make(chan eventMsg, 32)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *eventHub) unsubscribe(ch chan eventMsg) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *eventHub) publish(name string, data any) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- eventMsg{name: name, data: data}:
		default:
		}
	}
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := a.hub.subscribe()
	defer a.hub.unsubscribe(ch)

	send := func(name string, data any) bool {
		body, err := json.Marshal(data)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, body); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send("hello", map[string]any{
		"version": version,
		"modules": a.moduleNames(),
		"started": a.startedAt.Format(time.RFC3339),
		"qbUrl":   a.cfg.Load().QB.URL,
	}) {
		return
	}

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if !send(msg.name, msg.data) {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

type role int

const (
	roleAnyone role = iota
	roleRead
	roleWrite
)

func (a *App) guard(h http.HandlerFunc, need role) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isScanner(r) {
			fakeNginx404(w)
			return
		}
		if need != roleAnyone && !a.auth.authorized(w, r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录"})
			return
		}
		h(w, r)
	}
}

func (a *App) routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/login", a.guard(a.handleLogin, roleAnyone))
	mux.HandleFunc("POST /api/auth/logout", a.guard(a.handleLogout, roleAnyone))
	mux.HandleFunc("GET /api/auth/status", a.guard(a.handleAuthStatus, roleAnyone))

	mux.HandleFunc("GET /api/config", a.guard(a.handleGetConfig, roleRead))
	mux.HandleFunc("PUT /api/config", a.guard(a.handlePutConfig, roleWrite))
	mux.HandleFunc("GET /api/config/schema", a.guard(a.handleSchema, roleRead))
	mux.HandleFunc("POST /api/reload", a.guard(a.handleReload, roleWrite))

	mux.HandleFunc("GET /api/modules", a.guard(a.handleModules, roleRead))
	mux.HandleFunc("PUT /api/modules/{name}", a.guard(a.handlePutModule, roleWrite))
	mux.HandleFunc("POST /api/modules/{name}/test", a.guard(a.handleTestModule, roleRead))

	mux.HandleFunc("GET /api/rules/{module}", a.guard(a.handleGetRules, roleRead))
	mux.HandleFunc("PUT /api/rules/{module}", a.guard(a.handlePutRules, roleWrite))
	mux.HandleFunc("POST /api/rules/{module}/validate", a.guard(a.handleValidateRules, roleRead))

	mux.HandleFunc("GET /api/status", a.guard(a.handleStatus, roleRead))
	mux.HandleFunc("GET /api/bans", a.guard(a.handleBans, roleRead))
	mux.HandleFunc("POST /api/bans", a.guard(a.handleAddBan, roleWrite))
	mux.HandleFunc("DELETE /api/bans/{ip}", a.guard(a.handleDeleteBan, roleWrite))
	mux.HandleFunc("GET /api/stats", a.guard(a.handleStats, roleRead))
	mux.HandleFunc("GET /api/torrents", a.guard(a.handleTorrents, roleRead))

	mux.HandleFunc("GET /api/subscriptions", a.guard(a.handleSubscriptions, roleRead))
	mux.HandleFunc("POST /api/subscriptions/refresh", a.guard(a.handleRefreshSubscriptions, roleWrite))

	mux.HandleFunc("GET /api/trackers", a.guard(a.handleTrackers, roleRead))
	mux.HandleFunc("POST /api/trackers/refresh", a.guard(a.handleRefreshTrackers, roleWrite))

	mux.HandleFunc("GET /api/events", a.guard(a.handleEvents, roleRead))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "未知接口 " + r.URL.Path})
	})
	mux.HandleFunc("/", a.serveSPA)
	return mux
}

func (a *App) serveSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	assets, err := fs.Sub(webuiAssets, "webui/dist")
	if err != nil {
		http.Error(w, "WebUI 不可用", http.StatusInternalServerError)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" && name != "." {
		if info, err := fs.Stat(assets, name); err == nil && !info.IsDir() {
			http.ServeFileFS(w, r, assets, name)
			return
		}
	}
	http.ServeFileFS(w, r, assets, "index.html")
}

func (a *App) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !a.cfg.Load().Server.AllowCORS {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Allow-Credentials", "true")
		h.Add("Vary", "Origin")
		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) serveHTTP() {
	addr := a.cfg.Load().Server.listenAddr()
	go func() {
		logf("WebUI 已启动: http://%s/", addr)
		if err := http.ListenAndServe(addr, a.withCORS(a.routes())); err != nil {
			logf("HTTP 服务退出: %v", err)
		}
	}()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败: " + err.Error()})
		return false
	}
	return true
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	addr := clientAddr(r)
	if a.auth.limiter.blocked(addr) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "登录失败次数过多，请稍后再试"})
		return
	}
	var creds credentials
	if !readJSON(w, r, &creds) {
		return
	}
	cfg := a.cfg.Load()
	if !constantEqual(creds.Username, cfg.QB.Username) || !constantEqual(creds.Password, cfg.QB.Password) {
		a.auth.limiter.fail(addr)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "账号或密码错误"})
		return
	}
	a.auth.limiter.reset(addr)
	token := a.auth.issue()
	if token == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成会话失败"})
		return
	}
	setSessionCookie(w, token, a.auth.ttl)
	writeJSON(w, http.StatusOK, map[string]any{"username": cfg.QB.Username})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.auth.drop(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	authed := a.auth.authorized(w, r)
	resp := map[string]any{"authenticated": authed}
	if authed {
		resp["username"] = a.cfg.Load().QB.Username
	}
	writeJSON(w, http.StatusOK, resp)
}

var maskedPaths = map[string][]string{"qbittorrent": {"password"}}

const maskValue = "***"

func configToMap(cfg *Config) (map[string]any, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func maskSecrets(m map[string]any) {
	for section, keys := range maskedPaths {
		sub, _ := m[section].(map[string]any)
		for _, k := range keys {
			if _, ok := sub[k]; ok {
				sub[k] = maskValue
			}
		}
	}
}

func normalizeNumbers(v any) any {
	switch t := v.(type) {
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1<<53 {
			return int64(t)
		}
		return t
	case map[string]any:
		for k, sub := range t {
			t[k] = normalizeNumbers(sub)
		}
		return t
	case []any:
		for i, sub := range t {
			t[i] = normalizeNumbers(sub)
		}
		return t
	default:
		return v
	}
}

func stripMasked(patch map[string]any) {
	for section, keys := range maskedPaths {
		sub, _ := patch[section].(map[string]any)
		for _, k := range keys {
			if sub[k] == maskValue {
				delete(sub, k)
			}
		}
	}
}

func deepMerge(dst, src map[string]any) map[string]any {
	for k, v := range src {
		if v == nil {
			delete(dst, k)
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			if cur, ok := dst[k].(map[string]any); ok {
				dst[k] = deepMerge(cur, sub)
				continue
			}
			dst[k] = deepMerge(map[string]any{}, sub)
			continue
		}
		dst[k] = v
	}
	return dst
}

func (a *App) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	m, err := configToMap(a.cfg.Load())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	maskSecrets(m)
	writeJSON(w, http.StatusOK, m)
}

func (a *App) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if !readJSON(w, r, &patch) {
		return
	}
	restart, err := a.applyConfigPatch(patch)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "needRestart": restart})
}

func (a *App) applyConfigPatch(patch map[string]any) ([]string, error) {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()

	stripMasked(patch)
	if normalized, ok := normalizeNumbers(patch).(map[string]any); ok {
		patch = normalized
	}

	current, err := readConfigFile(a.configPath)
	if err != nil {
		return nil, err
	}
	merged := deepMerge(current, patch)

	data, err := yaml.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("序列化失败: %w", err)
	}

	tmp := a.configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return nil, fmt.Errorf("写入临时文件失败: %w", err)
	}
	next, err := loadConfig(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("配置校验失败（未落盘）: %w", err)
	}
	if err := os.Rename(tmp, a.configPath); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("替换配置文件失败: %w", err)
	}

	restart := a.restartFields(a.cfg.Load(), next)
	a.cfg.Store(next)
	if err := a.applyRuntime(); err != nil {
		return restart, fmt.Errorf("配置已写入，但热重载失败（当前仍按旧配置运行）: %w", err)
	}
	return restart, nil
}

func readConfigFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (a *App) restartFields(old, next *Config) []string {
	var out []string
	add := func(cond bool, name string) {
		if cond {
			out = append(out, name)
		}
	}
	add(old.Server.Address != next.Server.Address || old.Server.HTTP != next.Server.HTTP, "server.address / server.http")
	add(old.DataDir != next.DataDir, "data-dir")
	if out == nil {
		out = []string{}
	}
	return out
}

func (a *App) handleReload(w http.ResponseWriter, r *http.Request) {
	if err := a.applyRuntime(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "modules": a.moduleNames()})
}

type fieldSchema struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Unit    string   `json:"unit,omitempty"`
	Help    string   `json:"help,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Restart bool     `json:"restart,omitempty"`
	Options []string `json:"options,omitempty"`
}

type sectionSchema struct {
	Key    string        `json:"key"`
	Label  string        `json:"label"`
	Help   string        `json:"help,omitempty"`
	Fields []fieldSchema `json:"fields"`
}

func num(v float64) *float64 { return &v }

func moduleField(key, label, typ string) fieldSchema {
	return fieldSchema{Key: key, Label: label, Type: typ}
}

var baseModuleFields = []fieldSchema{
	{Key: "enabled", Label: "启用", Type: "bool"},
	{Key: "ban-duration", Label: "封禁时长", Type: "duration", Unit: "毫秒或 5s/72h/7d/2w",
		Help: "留空或填 default 表示跟随「全局」里的全局封禁时长"},
}

var schemaSections = []sectionSchema{
	{Key: "qbittorrent", Label: "下载器", Help: "面板连接的 qBittorrent / qB EE。改完即时重连，不必重启。", Fields: []fieldSchema{
		{Key: "url", Label: "WebAPI 地址", Type: "string", Help: "例如 http://127.0.0.1:8080。容器部署时用容器名，如 http://qbittorrentee:8080"},
		{Key: "username", Label: "用户名", Type: "string", Help: "同时是本面板的登录账号"},
		{Key: "password", Label: "密码", Type: "password", Help: "同时是本面板的登录密码"},
		{Key: "ban-method", Label: "封禁方式", Type: "select", Options: []string{"ban", "shadowban"},
			Help: "ban = 常规封禁（断开连接并写入封禁列表）；shadowban = 影子封禁，保持连接但不传数据，需要下载器开启 shadow_ban_enabled"},
	}},
	{Key: "server", Label: "WebUI", Fields: []fieldSchema{
		{Key: "address", Label: "监听地址", Type: "string", Restart: true, Help: "0.0.0.0 表示所有网卡都能访问，127.0.0.1 只允许本机"},
		{Key: "http", Label: "监听端口", Type: "int", Restart: true, Min: num(1), Max: num(65535)},
		{Key: "allow-cors", Label: "允许跨站", Type: "bool",
			Help: "仅在把前端单独部署到同主机的另一个端口时需要。会话 cookie 是 SameSite=Lax，跨主机的站点即使打开这项也无法登录"},
	}},
	{Key: "root", Label: "全局", Fields: []fieldSchema{
		{Key: "check-interval", Label: "检测间隔", Type: "duration", Unit: "毫秒或 5s/72h/7d/2w",
			Help: "多久跑一轮检测。调小响应更快但下载器压力更大，一般 10s–1m 足够"},
		{Key: "ban-duration", Label: "全局封禁时长", Type: "duration", Unit: "毫秒或 5s/72h/7d/2w",
			Help: "模块自己没写封禁时长时用这个"},
		{Key: "persist-interval", Label: "状态落盘间隔", Type: "duration", Unit: "毫秒或 5s/72h/7d/2w",
			Help: "检测状态写入磁盘的频率。掉电最多丢这个时长的进度记录"},
		{Key: "concurrency", Label: "并发检查数", Type: "int", Min: num(1), Max: num(64),
			Help: "同时检查多少个任务。任务多且下载器响应慢时可以调大"},
		{Key: "data-dir", Label: "数据目录", Type: "string", Restart: true,
			Help: "状态文件、流量历史、脚本与订阅缓存都放这里"},
		{Key: "ignore-peers-from-addresses", Label: "忽略的网段", Type: "stringlist",
			Help: "命中这些网段的 Peer 跳过全部检查。内网与自己的其它节点填这里，避免误伤"},
	}},
	{Key: "stats", Label: "流量统计", Help: "后台定时采样下载器，用于「流量统计」页。", Fields: []fieldSchema{
		{Key: "sample-interval", Label: "采样间隔", Type: "duration", Help: "多久采样一次。调小趋势更细但历史文件更大"},
		{Key: "recent-keep", Label: "近期保留", Type: "duration", Help: "采样粒度历史的保留时长，决定「近 48 小时」档的窗口"},
		{Key: "hourly-keep", Label: "小时保留", Type: "duration", Help: "按小时聚合历史的保留时长，决定「近 7 天」「近 30 天」两档的窗口"},
		{Key: "daily-keep", Label: "每日保留", Type: "duration", Help: "按天聚合历史的保留时长，决定「近 90 天」「近一年」「全部」三档的窗口。想看几年的趋势就调大它，没有上限"},
	}},
	{Key: "trackers", Label: "Tracker 订阅聚合", Fields: []fieldSchema{
		{Key: "enabled", Label: "启用", Type: "bool",
			Help: "启用后本面板接管下载器的「添加的 Tracker 列表」，你在下载器里手改的条目会被覆盖"},
		{Key: "refresh-interval", Label: "刷新间隔", Type: "duration", Unit: "毫秒或 24h/1d",
			Help: "多久重新拉一次订阅源。源列表变动很慢，一天一次足够"},
		{Key: "sources", Label: "订阅源", Type: "trackerSources"},
	}},
}

var schemaModules = []sectionSchema{
	{Key: "peer-id-blacklist", Label: "PeerID 黑名单",
		Help: "PeerID 是客户端连接时自报的身份串（qB 是 -qB 开头）。按规则匹配，命中即封。",
		Fields: append([]fieldSchema{
			moduleField("banned-peer-id", "规则列表", "rules"),
		}, baseModuleFields...)},
	{Key: "client-name-blacklist", Label: "客户端名称黑名单",
		Help: "客户端名称是对方自报的软件名（如「迅雷」「StellarPlayer」）。比 PeerID 更容易被伪造，但配合 PeerID 一起用命中率很高。",
		Fields: append([]fieldSchema{
			moduleField("banned-client-name", "规则列表", "rules"),
		}, baseModuleFields...)},
	{Key: "ip-address-blocker", Label: "IP / 端口黑名单",
		Help: "静态名单，命中即封，不做任何行为判断。适合封已知的吸血 IP 段或端口。",
		Fields: append([]fieldSchema{
			{Key: "ips", Label: "IP 或网段", Type: "stringlist",
				Help: "每行一个，支持单 IP（1.2.3.4）与 CIDR（1.2.3.0/24）。留空表示不按 IP 封"},
			{Key: "ports", Label: "端口", Type: "intlist",
				Help: "逗号或换行分隔。留空表示不按端口封"},
		}, baseModuleFields...)},
	{Key: "progress-cheat-blocker", Label: "虚假进度检查器",
		Help: "对比「我方实际上传了多少」与「对方汇报的进度」，判断对方是否在谎报下载量（下载完就跑的吸血行为）。这是唯一基于行为而非静态特征的检测。",
		Fields: append([]fieldSchema{
			{Key: "minimum-size", Label: "最小种子大小", Type: "int", Unit: "字节",
				Help: "小于此值的种子不检查。小种子上传量太小，判定不可靠"},
			{Key: "maximum-difference", Label: "最大进度差", Type: "float", Min: num(0), Max: num(1),
				Help: "对方汇报进度比我方算出的低多少就算作弊，0.1 表示差 10%。越小越严格，也越容易误判"},
			{Key: "rewind-maximum-difference", Label: "允许的进度倒退", Type: "float", Min: num(0), Max: num(1),
				Help: "对方汇报的进度比上次倒退多少就算作弊。设为 0 禁用"},
			{Key: "block-excessive-clients", Label: "拦截过量下载", Type: "bool",
				Help: "对方下载量明显超出种子大小时一并拦截"},
			{Key: "excessive-threshold", Label: "过量阈值", Type: "float", Min: num(1),
				Help: "下载量 ÷ 种子大小 超过这个倍数算过量，1.05 表示 105%"},
			{Key: "ipv4-prefix-length", Label: "IPv4 前缀长度", Type: "int", Min: num(1), Max: num(32),
				Help: "按网段聚合上传量，防止对方换 IP 洗掉记录。/24 表示同一 C 段合并计算"},
			{Key: "ipv6-prefix-length", Label: "IPv6 前缀长度", Type: "int", Min: num(1), Max: num(128),
				Help: "同上，IPv6 侧的聚合粒度"},
			{Key: "persist-duration", Label: "记录保留时长", Type: "duration",
				Help: "上传量记录保留多久。留太短会被「缓慢失忆攻击」绕过"},
			{Key: "max-wait-duration", Label: "补进度等待窗口", Type: "duration",
				Help: "发现进度落后后先等这么久，对方补上进度就不封。用于吸收网络抖动"},
			{Key: "fast-pcb-test-percentage", Label: "快速测试阈值", Type: "float", Min: num(0), Max: num(1),
				Help: "对方下载量超过种子大小的这个比例时主动断开一次，用于预热进度重置检查。设为 0 禁用"},
			{Key: "fast-pcb-test-block-duration", Label: "快速测试封禁时长", Type: "duration",
				Help: "上面那次断开的时长"},
		}, baseModuleFields...)},
	{Key: "auto-range-ban", Label: "自动连锁封禁",
		Help: "某个 IP 被封后，把它所在网段内的其它 IP 一起封掉。对付同一台机器频繁换 IP 的情况。",
		Fields: append([]fieldSchema{
			{Key: "ipv4", Label: "IPv4 前缀长度", Type: "int", Min: num(1), Max: num(32),
				Help: "连锁的网段大小，/30 表示连带封 4 个地址。调小风险大，别设得比 /24 还小"},
			{Key: "ipv6", Label: "IPv6 前缀长度", Type: "int", Min: num(1), Max: num(128),
				Help: "同上，IPv6 侧的网段大小"},
		}, baseModuleFields...)},
	{Key: "multi-dialing-blocker", Label: "多拨追猎",
		Help: "同一个网段里出现多个 IP 下载同一个任务时判定为多拨。正常的家宽 NAT 下偶尔会有，所以留了容忍数。",
		Fields: append([]fieldSchema{
			{Key: "subnet-mask-length", Label: "IPv4 网段长度", Type: "int", Min: num(1), Max: num(32),
				Help: "按这个粒度判断「是不是同一网段」，/24 表示同一 C 段算同一来源"},
			{Key: "subnet-mask-v6-length", Label: "IPv6 网段长度", Type: "int", Min: num(1), Max: num(128),
				Help: "同上，IPv6 侧的粒度"},
			{Key: "tolerate-num-ipv4", Label: "IPv4 容忍数", Type: "int", Min: num(0),
				Help: "同一网段内允许多少个 IPv4 同时下同一任务，超过才封。0 表示一个都不容忍"},
			{Key: "tolerate-num-ipv6", Label: "IPv6 容忍数", Type: "int", Min: num(0),
				Help: "同上，IPv6 侧"},
			{Key: "cache-lifespan", Label: "连接记录保留", Type: "duration",
				Help: "记录每个网段下过哪些任务，保留多久"},
			{Key: "keep-hunting", Label: "持续追猎", Type: "bool",
				Help: "判定为多拨后继续追猎该网段，而不是只封当前这些 IP"},
			{Key: "keep-hunting-time", Label: "追猎时长", Type: "duration",
				Help: "持续追猎持续多久"},
		}, baseModuleFields...)},
	{Key: "expression-engine", Label: "表达式规则引擎",
		Help:   "用脚本自己写判定逻辑，比固定规则灵活。脚本放在 <data-dir>/scripts/*.expr，改完在设置页点「重载模块」生效。",
		Fields: baseModuleFields},
	{Key: "ip-address-blocker-rules", Label: "IP 集规则订阅",
		Help: "从远程地址定期拉取 IP / 网段列表当黑名单用。源地址在「IP 集订阅」页管理。",
		Fields: append([]fieldSchema{
			{Key: "check-interval", Label: "检查间隔", Type: "duration",
				Help: "多久重新拉一次订阅源。列表变动很慢，几小时一次足够"},
			moduleField("rules", "订阅列表", "subscriptions"),
		}, baseModuleFields...)},
	{Key: "active-monitoring", Label: "主动监测",
		Help: "不参与封禁判定，只在流量异常时告警。由独立的监测循环承担，因此不会出现在总览的模块列表里。",
		Fields: append([]fieldSchema{
			{Key: "traffic-monitoring.daily", Label: "每日上传告警阈值", Type: "int", Unit: "字节",
				Help: "单日上传超过这个字节数就告警，用于发现被当成上传机的情况。-1 禁用"},
		}, baseModuleFields...)},
}

func (a *App) handleSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":  version,
		"sections": schemaSections,
		"modules":  schemaModules,
	})
}

var ruleFields = map[string]string{
	"peer-id-blacklist":     "banned-peer-id",
	"client-name-blacklist": "banned-client-name",
}

func moduleConfigMap(cfg *Config, name string) (map[string]any, bool) {
	all, err := configToMap(cfg)
	if err != nil {
		return nil, false
	}
	modules, _ := all["module"].(map[string]any)
	sub, ok := modules[name].(map[string]any)
	return sub, ok
}

func (a *App) handleModules(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Load()
	active := map[string]bool{}
	for _, n := range a.moduleNames() {
		active[n] = true
	}
	out := make([]map[string]any, 0, len(schemaModules))
	for _, def := range schemaModules {
		sub, present := moduleConfigMap(cfg, def.Key)
		if sub == nil {
			sub = map[string]any{}
		}
		out = append(out, map[string]any{
			"name":       def.Key,
			"label":      def.Label,
			"help":       def.Help,
			"configured": present,
			"active":     active[def.Key],
			"enabled":    sub["enabled"] == true,
			"config":     sub,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"modules": out})
}

func (a *App) handlePutModule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !knownModule(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "未知模块 " + name})
		return
	}
	var body map[string]any
	if !readJSON(w, r, &body) {
		return
	}
	restart, err := a.applyConfigPatch(map[string]any{"module": map[string]any{name: body}})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "needRestart": restart})
}

func knownModule(name string) bool {
	for _, def := range schemaModules {
		if def.Key == name {
			return true
		}
	}
	return false
}

func (a *App) handleGetRules(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("module")
	field, ok := ruleFields[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": name + " 不是规则列表型模块，请用 /api/modules/" + name,
		})
		return
	}
	sub, _ := moduleConfigMap(a.cfg.Load(), name)
	writeJSON(w, http.StatusOK, map[string]any{
		"module": name,
		"field":  field,
		"rules":  listOrEmpty(sub[field]),
	})
}

func listOrEmpty(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	return []any{}
}

func (a *App) handlePutRules(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("module")
	field, ok := ruleFields[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": name + " 不是规则列表型模块"})
		return
	}
	var body struct {
		Rules []string `json:"rules"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	restart, err := a.applyConfigPatch(map[string]any{
		"module": map[string]any{name: map[string]any{field: body.Rules}},
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(body.Rules), "needRestart": restart})
}

func (a *App) handleValidateRules(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("module")
	var body struct {
		Rules []string `json:"rules"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"module": name, "results": validateRules(body.Rules)})
}

type ruleCheck struct {
	Index int    `json:"index"`
	Rule  string `json:"rule"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Note  string `json:"note,omitempty"`
}

func validateRules(raw []string) []ruleCheck {
	out := make([]ruleCheck, 0, len(raw))
	for i, line := range raw {
		chk := ruleCheck{Index: i, Rule: line, OK: true}
		var one Rule
		if err := json.Unmarshal([]byte(line), &one); err != nil {
			chk.OK, chk.Error = false, "JSON 解析失败: "+err.Error()
			out = append(out, chk)
			continue
		}
		if err := one.compile(); err != nil {
			chk.OK, chk.Error = false, err.Error()
		} else {
			chk.Note = one.describe()
		}
		out = append(out, chk)
	}
	return out
}

type testPeerRequest struct {
	IP         string  `json:"ip"`
	Port       int     `json:"port"`
	PeerID     string  `json:"peerId"`
	ClientName string  `json:"clientName"`
	Progress   float64 `json:"progress"`
	UpSpeed    int64   `json:"upSpeed"`
	DlSpeed    int64   `json:"dlSpeed"`
	Uploaded   int64   `json:"uploaded"`
	Downloaded int64   `json:"downloaded"`
	Flags      string  `json:"flags"`
	Connection string  `json:"connection"`
}

type testTorrentRequest struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Private  bool    `json:"private"`
	Category string  `json:"category"`
	Tags     string  `json:"tags"`
}

type testRequest struct {
	Peer    testPeerRequest    `json:"peer"`
	Torrent testTorrentRequest `json:"torrent"`
}

func (a *App) handleTestModule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req testRequest
	if !readJSON(w, r, &req) {
		return
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(req.Peer.IP))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "peer.ip 无法解析: " + req.Peer.IP})
		return
	}
	addr = addr.Unmap()

	torrent := &Torrent{
		ID: req.Torrent.Hash, Name: req.Torrent.Name, Size: req.Torrent.Size,
		Progress: req.Torrent.Progress,
		Private:  req.Torrent.Private, Category: req.Torrent.Category, Tags: req.Torrent.Tags,
	}
	if torrent.Name == "" {
		torrent.Name = "（未命名种子）"
	}
	peer := &Peer{
		IP: req.Peer.IP, Port: req.Peer.Port, Client: req.Peer.ClientName,
		PeerIDStd: req.Peer.PeerID, Progress: req.Peer.Progress,
		UpSpeed: req.Peer.UpSpeed, DlSpeed: req.Peer.DlSpeed,
		Uploaded: req.Peer.Uploaded, Downloaded: req.Peer.Downloaded,
		Flags: req.Peer.Flags, Connection: req.Peer.Connection,
		Addr: addr, Key: req.Peer.IP,
	}

	if name == "all" {
		best, all := a.evaluate(torrent, peer)
		writeJSON(w, http.StatusOK, map[string]any{
			"ignored": a.ignoredAddr(addr),
			"banned":  a.bans.banned(addr),
			"results": describeResults(all),
			"winner":  describeResult(best),
		})
		return
	}

	rt := a.rt.Load()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "运行时未就绪"})
		return
	}
	var found Module
	for _, mod := range rt.modules {
		if mod.Name() == name {
			found = mod
			break
		}
	}
	if found == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"results": []any{},
			"note":    "模块 " + name + " 当前未启用，因此不会参与判定",
		})
		return
	}
	res := found.Check(torrent, peer)
	results := []*Result{}
	if res != nil {
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ignored": a.ignoredAddr(addr),
		"banned":  a.bans.banned(addr),
		"results": describeResults(results),
		"winner":  describeResult(res),
	})
}

func describeResults(list []*Result) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, res := range list {
		if res == nil {
			continue
		}
		out = append(out, describeResult(res))
	}
	return out
}

func describeResult(res *Result) map[string]any {
	if res == nil {
		return nil
	}
	return map[string]any{
		"module":     res.Module,
		"verdict":    verdictName(res.Verdict),
		"reason":     res.Reason,
		"durationMs": res.Duration.Milliseconds(),
		"duration":   formatDuration(res.Duration),
		"detail":     res.Detail,
	}
}

func verdictName(v verdict) string {
	switch v {
	case verdictBan:
		return "ban"
	case verdictDisconnect:
		return "disconnect"
	case verdictSkip:
		return "skip"
	default:
		return "none"
	}
}

func (a *App) statusPayload() statusPayload {
	cfg := a.cfg.Load()
	rt := a.rt.Load()
	bans := a.bans.snapshot()
	payload := statusPayload{
		Version:        version,
		Uptime:         time.Since(a.startedAt).Round(time.Second).String(),
		QBURL:          cfg.QB.URL,
		BanMethod:      cfg.QB.BanMethod,
		CheckEvery:     time.Duration(cfg.CheckInterval).String(),
		Cycles:         loadInt64(&a.counters.cycles),
		ActiveTorrents: loadInt64(&a.counters.torrents),
		PeersChecked:   loadInt64(&a.counters.peers),
		BansIssued:     loadInt64(&a.counters.banned),
		Skipped:        loadInt64(&a.counters.skipped),
		AlreadyBanned:  loadInt64(&a.counters.alreadyBanned),
		Errors:         loadInt64(&a.counters.errors),
		CycleTakenMs:   a.cycleTakenNs.Load() / int64(time.Millisecond),
		CurrentBans:    len(bans),
		Modules:        a.moduleNames(),
		Bans:           bans,
	}
	if ns := a.lastCycleNs.Load(); ns > 0 {
		payload.LastCycle = time.Unix(0, ns).Format("15:04:05")
	}
	if rt != nil && rt.subs != nil {
		payload.Subscriptions = rt.subs.Size()
	}
	a.st.mu.Lock()
	if entry := a.st.Traffic[time.Now().Format("2006-01-02")]; entry != nil {
		copyEntry := *entry
		payload.TrafficToday = &copyEntry
	}
	a.st.mu.Unlock()
	return payload
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.statusPayload())
}

func (a *App) handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.stats.payload())
}

func (a *App) handleTorrents(w http.ResponseWriter, r *http.Request) {
	list, err := a.qb.Torrents()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "读取任务列表失败: " + err.Error()})
		return
	}
	rows := make([]torrentView, 0, len(list))
	for _, t := range list {
		rows = append(rows, torrentView{Torrent: t, Site: siteOf(t.Tracker)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"torrents": rows, "now": time.Now().Unix()})
}

type torrentView struct {
	Torrent
	Site string `json:"site"`
}

func (a *App) handleBans(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("module")
	records := a.bans.snapshot()
	if filter != "" {
		kept := make([]BanRecord, 0, len(records))
		for _, rec := range records {
			if rec.Module == filter {
				kept = append(kept, rec)
			}
		}
		records = kept
	}
	a.st.mu.Lock()
	foreign := make([]string, 0, len(a.bans.foreign))
	for ip := range a.bans.foreign {
		foreign = append(foreign, ip)
	}
	a.st.mu.Unlock()
	slices.Sort(foreign)
	writeJSON(w, http.StatusOK, map[string]any{"bans": records, "foreign": foreign})
}

type banRequest struct {
	IP       string `json:"ip"`
	Duration string `json:"duration"`
	Reason   string `json:"reason"`
}

func (a *App) handleAddBan(w http.ResponseWriter, r *http.Request) {
	var req banRequest
	if !readJSON(w, r, &req) {
		return
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(req.IP))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "IP 无法解析: " + req.IP})
		return
	}
	dur := time.Duration(a.cfg.Load().BanDuration)
	if strings.TrimSpace(req.Duration) != "" {
		dur, err = parseDurationFlexible(req.Duration)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "时长无法解析: " + err.Error()})
			return
		}
	}
	if dur <= 0 {
		dur = 14 * 24 * time.Hour
	}
	reason := req.Reason
	if reason == "" {
		reason = "手动封禁"
	}
	record := a.bans.addManual(addr.Unmap().String(), time.Now().Add(dur), reason)
	a.hub.publish("ban", map[string]any{"action": "add", "record": record})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "record": record})
}

func (a *App) handleDeleteBan(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	if _, err := netip.ParseAddr(ip); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "IP 无法解析: " + ip})
		return
	}
	if !a.bans.remove(ip) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": ip + " 不在封禁列表中"})
		return
	}
	a.hub.publish("ban", map[string]any{"action": "remove", "ip": ip})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func parseDurationFlexible(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if ms, err := parsePositiveInt(s); err == nil {
		return time.Duration(ms) * time.Millisecond, nil
	}
	return parseDuration(s)
}

func parsePositiveInt(s string) (int64, error) {
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
		return 0, fmt.Errorf("不是正整数")
	}
	return n, nil
}

func (a *App) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Load()
	subs := []any{}
	if c := cfg.Modules.IPRuleList; c != nil {
		for _, sub := range c.subscriptions() {
			entry := map[string]any{
				"id": sub.ID, "name": sub.Name, "url": sub.URL, "enabled": sub.Enabled,
			}
			if rt := a.rt.Load(); rt != nil && rt.subs != nil {
				entry["prefixes"] = rt.subs.Count(sub.ID)
			}
			subs = append(subs, entry)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"subscriptions": subs,
		"enabled":       cfg.Modules.IPRuleList != nil && cfg.Modules.IPRuleList.Enabled,
	})
}

func (a *App) handleRefreshSubscriptions(w http.ResponseWriter, r *http.Request) {
	rt := a.rt.Load()
	if rt == nil || rt.subs == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "IP 集订阅模块未启用"})
		return
	}
	go rt.subs.Refresh()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func resolveConfigPath(arg string) string {
	if filepath.IsAbs(arg) {
		return arg
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return arg
	}
	return abs
}
