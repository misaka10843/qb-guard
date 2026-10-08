package main

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type banEvent struct {
	Ts     int64
	IP     string
	Module string
}

type banLog struct {
	mu     sync.Mutex
	path   string
	events []banEvent
	dirty  bool
}

func newBanLog(dir string) *banLog {
	return &banLog{path: filepath.Join(dir, "bans.csv")}
}

func (l *banLog) load() error {
	file, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	events := make([]banEvent, 0, 1024)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ",")
		if len(fields) != 3 {
			continue
		}
		ts := parseInt(fields[0])
		if ts <= 0 || fields[1] == "" {
			continue
		}
		events = append(events, banEvent{Ts: ts, IP: fields[1], Module: fields[2]})
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	l.events = events
	l.mu.Unlock()
	return nil
}

func (l *banLog) add(ip, module string) {
	l.mu.Lock()
	l.events = append(l.events, banEvent{Ts: time.Now().Unix(), IP: ip, Module: module})
	l.dirty = true
	l.mu.Unlock()
}

func (l *banLog) save(keep time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-keep).Unix()
	kept := l.events[:0]
	for _, event := range l.events {
		if event.Ts >= cutoff {
			kept = append(kept, event)
		}
	}
	trimmed := len(kept) != len(l.events)
	l.events = kept
	if !l.dirty && !trimmed {
		return nil
	}
	var builder strings.Builder
	for _, event := range l.events {
		fmt.Fprintf(&builder, "%d,%s,%s\n", event.Ts, event.IP, event.Module)
	}
	if err := writeAtomic(l.path, []byte(builder.String())); err != nil {
		return err
	}
	l.dirty = false
	return nil
}

func (l *banLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.events)
}

type geoBucket struct {
	Code    string         `json:"code"`
	Count   int            `json:"count"`
	Modules map[string]int `json:"modules"`
	LastTs  int64          `json:"lastTs"`
	IPs     []string       `json:"ips"`
}

type geoDay struct {
	Ts    int64 `json:"ts"`
	Count int   `json:"count"`
}

type geoAggregate struct {
	Total   int         `json:"total"`
	Unknown int         `json:"unknown"`
	Buckets []geoBucket `json:"countries"`
	Days    []geoDay    `json:"series"`
}

func (l *banLog) aggregate(cutoff int64, lookup func(netip.Addr) string, ipLimit int) geoAggregate {
	l.mu.Lock()
	events := make([]banEvent, len(l.events))
	copy(events, l.events)
	l.mu.Unlock()

	byCode := map[string]*geoBucket{}
	byDay := map[int64]int{}
	resolved := map[string]string{}
	result := geoAggregate{}

	for _, event := range events {
		if event.Ts < cutoff {
			continue
		}
		result.Total++
		code, cached := resolved[event.IP]
		if !cached {
			addr, err := netip.ParseAddr(event.IP)
			if err != nil {
				code = ""
			} else {
				code = lookup(addr)
			}
			resolved[event.IP] = code
		}
		if code == "" {
			result.Unknown++
		} else {
			bucket, ok := byCode[code]
			if !ok {
				bucket = &geoBucket{Code: code, Modules: map[string]int{}}
				byCode[code] = bucket
			}
			bucket.Count++
			bucket.Modules[event.Module]++
			if event.Ts > bucket.LastTs {
				bucket.LastTs = event.Ts
			}
			if ipLimit <= 0 || len(bucket.IPs) < ipLimit {
				bucket.IPs = append(bucket.IPs, event.IP)
			}
		}
		byDay[dayStart(event.Ts)]++
	}

	result.Buckets = make([]geoBucket, 0, len(byCode))
	for _, bucket := range byCode {
		result.Buckets = append(result.Buckets, *bucket)
	}
	sort.Slice(result.Buckets, func(i, j int) bool {
		if result.Buckets[i].Count != result.Buckets[j].Count {
			return result.Buckets[i].Count > result.Buckets[j].Count
		}
		return result.Buckets[i].Code < result.Buckets[j].Code
	})

	result.Days = make([]geoDay, 0, len(byDay))
	for day, count := range byDay {
		result.Days = append(result.Days, geoDay{Ts: day, Count: count})
	}
	sort.Slice(result.Days, func(i, j int) bool { return result.Days[i].Ts < result.Days[j].Ts })
	return result
}
