package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Torrent struct {
	ID          string  `json:"hash"`
	Name        string  `json:"name"`
	Size        int64   `json:"size"`
	Completed   int64   `json:"completed"`
	Progress    float64 `json:"progress"`
	State       string  `json:"state"`
	Private     bool    `json:"private"`
	UpSpeed     int64   `json:"upspeed"`
	DlSpeed     int64   `json:"dlspeed"`
	Category    string  `json:"category"`
	Tags        string  `json:"tags"`
	Tracker     string  `json:"tracker"`
	Uploaded    int64   `json:"uploaded"`
	Downloaded  int64   `json:"downloaded"`
	SeedingTime int64   `json:"seeding_time"`

	TotalSize    int64   `json:"total_size"`
	UpSession    int64   `json:"uploaded_session"`
	DlSession    int64   `json:"downloaded_session"`
	NumSeeds     int     `json:"num_seeds"`
	NumLeechs    int     `json:"num_leechs"`
	NumComplete  int     `json:"num_complete"`
	NumIncomplet int     `json:"num_incomplete"`
	AddedOn      int64   `json:"added_on"`
	CompletedOn  int64   `json:"completion_on"`
	LastActive   int64   `json:"last_activity"`
	TimeActive   int64   `json:"time_active"`
	AmountLeft   int64   `json:"amount_left"`
	Eta          int64   `json:"eta"`
	Availability float64 `json:"availability"`
	SavePath     string  `json:"save_path"`
	ContentPath  string  `json:"content_path"`
}

type Peer struct {
	IP         string  `json:"ip"`
	Port       int     `json:"port"`
	Client     string  `json:"client"`
	PeerIDEE   string  `json:"peer_id_client"`
	PeerIDStd  string  `json:"peer_id"`
	Progress   float64 `json:"progress"`
	UpSpeed    int64   `json:"up_speed"`
	DlSpeed    int64   `json:"dl_speed"`
	Uploaded   int64   `json:"uploaded"`
	Downloaded int64   `json:"downloaded"`
	Flags      string  `json:"flags"`
	Connection string  `json:"connection"`
	Shadowban  bool    `json:"shadowbanned"`

	Key  string     `json:"-"`
	Addr netip.Addr `json:"-"`
}

func (p *Peer) PeerID() string {
	if p.PeerIDEE != "" {
		return p.PeerIDEE
	}
	return p.PeerIDStd
}

func (p *Peer) Handshaking() bool { return p.UpSpeed <= 0 && p.DlSpeed <= 0 }

func (p *Peer) UploadingToPeer() bool { return p.UpSpeed > 0 || p.Uploaded > 0 }

func (p *Peer) IPString() string { return p.Addr.String() }

type MainData struct {
	RID         int `json:"rid"`
	ServerState struct {
		AlltimeUL   int64 `json:"alltime_ul"`
		AlltimeDL   int64 `json:"alltime_dl"`
		UpInfoSpeed int64 `json:"up_info_speed"`
		DlInfoSpeed int64 `json:"dl_info_speed"`
	} `json:"server_state"`
}

type QBClient struct {
	cfg  func() QBConfig
	http *http.Client

	loginMu sync.Mutex
	logged  bool
	base    string
}

func newQBClient(cfg func() QBConfig) (*QBClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &QBClient{
		cfg:  cfg,
		http: &http.Client{Jar: jar, Timeout: 30 * time.Second},
	}, nil
}

func (q *QBClient) endpoint() string {
	return strings.TrimRight(q.cfg().URL, "/")
}

func (q *QBClient) login() error {
	q.loginMu.Lock()
	defer q.loginMu.Unlock()

	c := q.cfg()
	base := strings.TrimRight(c.URL, "/")
	form := url.Values{"username": {c.Username}, "password": {c.Password}}
	resp, err := q.http.PostForm(base+"/api/v2/auth/login", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == http.StatusNoContent:
	case resp.StatusCode == http.StatusOK && strings.Contains(strings.ToLower(string(body)), "ok"):
	default:
		return fmt.Errorf("登录 HTTP %d: %s", resp.StatusCode, shorten(body))
	}
	q.logged = true
	q.base = base
	return nil
}

func (q *QBClient) ensureLogin() error {
	q.loginMu.Lock()
	stale := q.base != strings.TrimRight(q.cfg().URL, "/")
	logged := q.logged && !stale
	q.loginMu.Unlock()
	if logged {
		return nil
	}
	return q.login()
}

func (q *QBClient) send(method, path string, form url.Values) ([]byte, int, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, q.endpoint()+path, body)
	if err != nil {
		return nil, 0, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := q.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

func (q *QBClient) do(method, path string, form url.Values, out any) error {
	if err := q.ensureLogin(); err != nil {
		return err
	}
	data, status, err := q.send(method, path, form)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		if err := q.login(); err != nil {
			return err
		}
		if data, status, err = q.send(method, path, form); err != nil {
			return err
		}
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%s %s: HTTP %d %s", method, path, status, shorten(data))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func (q *QBClient) Torrents() ([]Torrent, error) {
	var list []Torrent
	err := q.do(http.MethodGet, "/api/v2/torrents/info", nil, &list)
	return list, err
}

func (q *QBClient) Peers(hash string) ([]Peer, error) {
	var raw struct {
		Peers map[string]*Peer `json:"peers"`
	}
	err := q.do(http.MethodGet, "/api/v2/sync/torrentPeers?hash="+url.QueryEscape(hash), nil, &raw)
	if err != nil {
		return nil, err
	}
	peers := make([]Peer, 0, len(raw.Peers))
	for key, p := range raw.Peers {
		if p.Shadowban || p.IP == "" {
			continue
		}
		switch strings.ToLower(p.Connection) {
		case "http", "https", "web":
			continue
		}
		if strings.Contains(key, ".onion") || strings.Contains(key, ".i2p") {
			continue
		}
		addr, err := netip.ParseAddr(strings.Trim(p.IP, "[]"))
		if err != nil {
			continue
		}
		p.Addr = addr.Unmap()
		p.Key = key
		peers = append(peers, *p)
	}
	return peers, nil
}

func (q *QBClient) BanPeers(keys []string, shadow bool) error {
	if len(keys) == 0 {
		return nil
	}
	path := "/api/v2/transfer/banPeers"
	if shadow {
		path = "/api/v2/transfer/shadowbanPeers"
	}
	return q.do(http.MethodPost, path, url.Values{"peers": {strings.Join(keys, "|")}}, nil)
}

func (q *QBClient) SetBanList(ips []string, shadow bool) error {
	field := "banned_IPs"
	if shadow {
		field = "shadow_banned_IPs"
	}
	payload, err := json.Marshal(map[string]string{field: strings.Join(ips, "\n")})
	if err != nil {
		return err
	}
	return q.do(http.MethodPost, "/api/v2/app/setPreferences", url.Values{"json": {string(payload)}}, nil)
}

func (q *QBClient) MainData(rid int) (*MainData, error) {
	var data MainData
	err := q.do(http.MethodGet, "/api/v2/sync/maindata?rid="+strconv.Itoa(rid), nil, &data)
	return &data, err
}

func (q *QBClient) Preferences() (map[string]any, error) {
	var prefs map[string]any
	err := q.do(http.MethodGet, "/api/v2/app/preferences", nil, &prefs)
	return prefs, err
}

func shorten(data []byte) string {
	s := strings.TrimSpace(string(bytes.TrimSpace(data)))
	if len(s) > 160 {
		return s[:160] + "..."
	}
	return s
}
