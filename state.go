

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type pcbStats struct {
	TrackingUploaded          int64   `json:"tu"`
	LastReportUploaded        int64   `json:"lru"`
	LastReportProgress        float64 `json:"lrp"`
	LastTorrentCompletedSize  int64   `json:"ltc"`
	ProgressDifferenceCounter int     `json:"pdc"`
	RewindCounter             int     `json:"rw"`
	BanDelayWindowEndAt       int64   `json:"bdw"`
	FastPcbTestExecuteAt      int64   `json:"fpt"`
	LastSeen                  int64   `json:"ls"`
}

type pcbRange struct {
	TorrentID string `json:"t"`
	Prefix    string `json:"p"`
	pcbStats
}

type pcbAddr struct {
	TorrentID string `json:"t"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	pcbStats
}

type BanRecord struct {
	IP         string `json:"ip"`
	Module     string `json:"module"`
	Reason     string `json:"reason"`
	Torrent    string `json:"torrent"`
	UntilMs    int64  `json:"untilMs"`
	Disconnect bool   `json:"disconnect"`
}

type TrafficDay struct {
	Uploaded   int64 `json:"up"`
	Downloaded int64 `json:"dl"`
}

type State struct {
	Ranges  map[string]*pcbRange        `json:"pcb_ranges"`
	Addrs   map[string]*pcbAddr         `json:"pcb_addrs"`
	Bans    map[string]*BanRecord       `json:"bans"`
	Subnets map[string]map[string]int64 `json:"mdd_subnets"`
	Hunting map[string]int64            `json:"mdd_hunting"`
	Traffic map[string]*TrafficDay      `json:"traffic"`
	RID     int                         `json:"rid"`
	LastUL  int64                       `json:"last_alltime_ul"`
	LastDL  int64                       `json:"last_alltime_dl"`

	path  string
	mu    sync.Mutex
	dirty bool
}

func loadState(path string) (*State, error) {
	s := &State{
		Ranges:  map[string]*pcbRange{},
		Addrs:   map[string]*pcbAddr{},
		Bans:    map[string]*BanRecord{},
		Subnets: map[string]map[string]int64{},
		Hunting: map[string]int64{},
		Traffic: map[string]*TrafficDay{},
		path:    path,
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	s.path = path
	if s.Ranges == nil {
		s.Ranges = map[string]*pcbRange{}
	}
	if s.Addrs == nil {
		s.Addrs = map[string]*pcbAddr{}
	}
	if s.Bans == nil {
		s.Bans = map[string]*BanRecord{}
	}
	if s.Subnets == nil {
		s.Subnets = map[string]map[string]int64{}
	}
	if s.Hunting == nil {
		s.Hunting = map[string]int64{}
	}
	if s.Traffic == nil {
		s.Traffic = map[string]*TrafficDay{}
	}
	return s, nil
}

func (s *State) save() error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeAtomic(s.path, data)
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *State) flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	s.dirty = false
	return s.save()
}

func (s *State) flushLoop(every func() time.Duration, stop <-chan struct{}) {
	current := every()
	ticker := time.NewTicker(current)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if next := every(); next != current {
				current = next
				ticker.Reset(current)
			}
			if err := s.flush(); err != nil {
				logf("状态落盘失败: %v", err)
			}
		case <-stop:
			return
		}
	}
}

func (s *State) prune(persist time.Duration) int {
	cutoff := time.Now().Add(-persist).UnixMilli()
	removed := 0
	for k, v := range s.Ranges {
		if v.LastSeen < cutoff {
			delete(s.Ranges, k)
			removed++
		}
	}
	for k, v := range s.Addrs {
		if v.LastSeen < cutoff {
			delete(s.Addrs, k)
			removed++
		}
	}
	if removed > 0 {
		s.dirty = true
	}
	return removed
}
