package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type rateEntry struct {
	start time.Time
	count int
}
type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]rateEntry
}

func (l *rateLimiter) allow(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = map[string]rateEntry{}
	}
	now := time.Now()
	e := l.entries[key]
	if now.Sub(e.start) >= time.Minute {
		e = rateEntry{start: now}
	}
	if e.count >= limit {
		return false
	}
	if len(l.entries) >= 4096 {
		for k, v := range l.entries {
			if now.Sub(v.start) >= time.Minute {
				delete(l.entries, k)
			}
		}
		if len(l.entries) >= 4096 && e.count == 0 {
			return false
		}
	}
	e.count++
	l.entries[key] = e
	return true
}
func remoteIP(r *http.Request) string {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return r.RemoteAddr
	}
	return host
}
