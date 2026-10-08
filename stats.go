package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SiteStat struct {
	Site         string `json:"site"`
	Total        int    `json:"total"`
	Seeding      int    `json:"seeding"`
	SeedingBytes int64  `json:"seeding_bytes"`
	Uploaded     int64  `json:"uploaded"`
	Downloaded   int64  `json:"downloaded"`
}

type GlobalStat struct {
	Uploaded     int64 `json:"uploaded"`
	Downloaded   int64 `json:"downloaded"`
	UpSpeed      int64 `json:"upspeed"`
	DlSpeed      int64 `json:"dlspeed"`
	Seeding      int   `json:"seeding"`
	SeedingBytes int64 `json:"seeding_bytes"`
	Total        int   `json:"total"`
	Sites        int   `json:"sites"`
}

type StatsPoint struct {
	Ts         int64 `json:"ts"`
	Uploaded   int64 `json:"uploaded"`
	Downloaded int64 `json:"downloaded"`
	Seeding    int   `json:"seeding"`
	Total      int   `json:"total"`
	Sites      int   `json:"sites"`
}

type DayStat struct {
	Ts         int64 `json:"ts"`
	Uploaded   int64 `json:"uploaded"`
	Downloaded int64 `json:"downloaded"`
}

type StatsTrend struct {
	Recent []StatsPoint `json:"recent"`
	Hourly []StatsPoint `json:"hourly"`
	Daily  []StatsPoint `json:"daily"`
	Days   []DayStat    `json:"days"`
}

type StatsPayload struct {
	Ts     int64      `json:"ts"`
	Global GlobalStat `json:"global"`
	Sites  []SiteStat `json:"sites"`
	Trend  StatsTrend `json:"trend"`
}

type statsStore struct {
	mu       sync.Mutex
	latest   StatsPayload
	recent   []StatsPoint
	hourly   []StatsPoint
	daily    []StatsPoint
	lastHour int64
	lastDay  int64
	dir      string
	window   func() statsWindow
}

type statsWindow struct {
	recent time.Duration
	hourly time.Duration
	daily  time.Duration
}

var seedingStates = map[string]bool{
	"uploading": true, "stalledUP": true, "queuedUP": true,
	"forcedUP": true, "checkingUP": true, "pausedUP": true,
}

func siteOf(tracker string) string {
	parsed, err := url.Parse(tracker)
	if err != nil || parsed.Hostname() == "" {
		return "其他"
	}
	host := parsed.Hostname()
	parts := strings.Split(host, ".")
	if len(parts) >= 3 && !isDigits(strings.ReplaceAll(host, ".", "")) {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return host
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func newStatsStore(dir string, window func() statsWindow) *statsStore {
	return &statsStore{
		dir:    dir,
		window: window,
		latest: StatsPayload{Sites: []SiteStat{}},
	}
}

func (s *statsStore) load() error {
	return s.loadGlobal()
}

func (s *statsStore) loadGlobal() error {
	file, err := os.Open(filepath.Join(s.dir, "global.csv"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ",")
		if len(fields) != 7 {
			continue
		}
		point := StatsPoint{
			Ts:         parseInt(fields[1]),
			Uploaded:   parseInt(fields[2]),
			Downloaded: parseInt(fields[3]),
			Seeding:    int(parseInt(fields[4])),
			Total:      int(parseInt(fields[5])),
			Sites:      int(parseInt(fields[6])),
		}
		switch fields[0] {
		case "5m":
			s.recent = append(s.recent, point)
		case "1h":
			s.hourly = append(s.hourly, point)
		case "1d":
			s.daily = append(s.daily, point)
		}
	}
	if n := len(s.recent); n > 0 {
		s.latest.Ts = s.recent[n-1].Ts
	}
	if n := len(s.hourly); n > 0 {
		s.lastHour = s.hourly[n-1].Ts
	}
	if n := len(s.daily); n > 0 {
		s.lastDay = s.daily[n-1].Ts
	}
	return scanner.Err()
}

func (s *statsStore) save() error {
	return s.saveGlobal()
}

func (s *statsStore) saveGlobal() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var builder strings.Builder
	write := func(res string, point StatsPoint) {
		fmt.Fprintf(&builder, "%s,%d,%d,%d,%d,%d,%d\n",
			res, point.Ts, point.Uploaded, point.Downloaded, point.Seeding, point.Total, point.Sites)
	}
	for _, point := range s.recent {
		write("5m", point)
	}
	for _, point := range s.hourly {
		write("1h", point)
	}
	for _, point := range s.daily {
		write("1d", point)
	}
	return writeAtomic(filepath.Join(s.dir, "global.csv"), []byte(builder.String()))
}

func (s *statsStore) sample(torrents []Torrent, state MainData) {
	now := time.Now().Unix()
	totals := map[string]*SiteStat{}
	seeding, seedingBytes := 0, int64(0)
	for i := range torrents {
		torrent := &torrents[i]
		site := siteOf(torrent.Tracker)
		stat, ok := totals[site]
		if !ok {
			stat = &SiteStat{Site: site}
			totals[site] = stat
		}
		stat.Total++
		stat.Uploaded += torrent.Uploaded
		stat.Downloaded += torrent.Downloaded
		if seedingStates[torrent.State] {
			stat.Seeding++
			stat.SeedingBytes += torrent.Size
			seeding++
			seedingBytes += torrent.Size
		}
	}
	sites := make([]SiteStat, 0, len(totals))
	for _, stat := range totals {
		sites = append(sites, *stat)
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].Uploaded > sites[j].Uploaded })

	point := StatsPoint{
		Ts:         now,
		Uploaded:   state.ServerState.AlltimeUL,
		Downloaded: state.ServerState.AlltimeDL,
		Seeding:    seeding,
		Total:      len(torrents),
		Sites:      len(sites),
	}

	s.mu.Lock()
	s.latest = StatsPayload{
		Ts: now,
		Global: GlobalStat{
			Uploaded:     state.ServerState.AlltimeUL,
			Downloaded:   state.ServerState.AlltimeDL,
			UpSpeed:      state.ServerState.UpInfoSpeed,
			DlSpeed:      state.ServerState.DlInfoSpeed,
			Seeding:      seeding,
			SeedingBytes: seedingBytes,
			Total:        len(torrents),
			Sites:        len(sites),
		},
		Sites: sites,
	}
	s.recent = append(s.recent, point)
	if hourStart(now) != hourStart(s.lastHour) {
		s.lastHour = now
		s.hourly = append(s.hourly, point)
	}
	if dayStart(now) != dayStart(s.lastDay) {
		s.lastDay = now
		s.daily = append(s.daily, point)
	}
	w := s.window()
	s.recent = trimStatsPoints(s.recent, now-seconds(w.recent))
	s.hourly = trimStatsPoints(s.hourly, now-seconds(w.hourly))
	s.daily = trimStatsPoints(s.daily, now-seconds(w.daily))
	s.mu.Unlock()
}

func (s *statsStore) payload() StatsPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := s.latest
	recent := deltaPoints(s.recent)
	hourly := deltaPoints(s.hourly)
	daily := deltaPoints(s.daily)
	result.Trend = StatsTrend{
		Recent: recent,
		Hourly: hourly,
		Daily:  daily,
		Days:   dayRollup(recent, hourly, daily),
	}
	return result
}

func dayRollup(recent, hourly, daily []StatsPoint) []DayStat {
	const (
		levelRecent = iota
		levelHourly
		levelDaily
	)
	byDay := map[int64]*DayStat{}
	levelOf := map[int64]int{}
	put := func(points []StatsPoint, level int) {
		for _, point := range points {
			day := dayStart(point.Ts)
			current, seen := byDay[day]
			if !seen {
				byDay[day] = &DayStat{Ts: day, Uploaded: point.Uploaded, Downloaded: point.Downloaded}
				levelOf[day] = level
				continue
			}
			if level < levelOf[day] {
				current.Uploaded = point.Uploaded
				current.Downloaded = point.Downloaded
				levelOf[day] = level
				continue
			}
			if level == levelOf[day] {
				current.Uploaded += point.Uploaded
				current.Downloaded += point.Downloaded
			}
		}
	}
	put(daily, levelDaily)
	put(hourly, levelHourly)
	put(recent, levelRecent)

	days := make([]DayStat, 0, len(byDay))
	for _, day := range byDay {
		days = append(days, *day)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Ts < days[j].Ts })
	return days
}

func deltaPoints(points []StatsPoint) []StatsPoint {
	out := make([]StatsPoint, 0, len(points))
	for i := 1; i < len(points); i++ {
		point := points[i]
		point.Uploaded -= points[i-1].Uploaded
		point.Downloaded -= points[i-1].Downloaded
		if point.Uploaded < 0 {
			point.Uploaded = 0
		}
		if point.Downloaded < 0 {
			point.Downloaded = 0
		}
		out = append(out, point)
	}
	return out
}

func hourStart(ts int64) int64 { return ts - ts%3600 }

func dayStart(ts int64) int64 {
	t := time.Unix(ts, 0)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).Unix()
}

func seconds(d time.Duration) int64 {
	return int64(d / time.Second)
}

func trimStatsPoints(points []StatsPoint, cutoff int64) []StatsPoint {
	kept := points[:0]
	for _, point := range points {
		if point.Ts >= cutoff {
			kept = append(kept, point)
		}
	}
	return kept
}

func parseInt(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}
