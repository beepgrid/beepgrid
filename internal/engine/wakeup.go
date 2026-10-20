package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"sync"
	"time"
)

type wakeEntry struct {
	ready chan struct{}
	refs  int
}
type wakeHub struct {
	mu      sync.Mutex
	entries map[string]*wakeEntry
}

func (h *wakeHub) watch(key string) (<-chan struct{}, func()) {
	h.mu.Lock()
	if h.entries == nil {
		h.entries = map[string]*wakeEntry{}
	}
	e := h.entries[key]
	if e == nil {
		e = &wakeEntry{ready: make(chan struct{})}
		h.entries[key] = e
	}
	e.refs++
	h.mu.Unlock()
	return e.ready, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		e.refs--
		if e.refs == 0 && h.entries[key] == e {
			delete(h.entries, key)
		}
	}
}
func (h *wakeHub) signal(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e := h.entries[key]; e != nil {
		close(e.ready)
		delete(h.entries, key)
	}
}
func notifyWork(ctx context.Context, db DB, scope, recipient string) error {
	if recipient == "" {
		return nil
	}
	_, e := db.Exec(ctx, `SELECT pg_notify('notif_work',$1)`, string(JSON([]string{scope, recipient})))
	return e
}
func (s *Store) WatchWork(a Actor) (<-chan struct{}, func()) {
	return s.wakes.watch(a.Scope + "/" + a.ID)
}

// WatchEvents registers before the durable read to avoid the read/watch race.
// Hints carry only a workspace ID, are coalesced per transaction by PostgreSQL,
// and authorize nothing. Consumers must recheck credentials and visibility.
func (s *Store) WatchEvents(a Actor) (<-chan struct{}, func()) {
	return s.eventWakes.watch(a.Scope)
}

// WatchProviders is a process-wide, coalesced hint for the bounded dispatcher.
// Retry deadlines, recovery and missed notifications still use periodic scans.
func (s *Store) WatchProviders() (<-chan struct{}, func()) {
	return s.providerWakes.watch("")
}

// Notifications are a wakeup hint only. PostgreSQL inbox rows remain authoritative and periodic reads recover missed hints.
func (s *Store) startWakeups() {
	ctx, cancel := context.WithCancel(context.Background())
	s.stopWakeups = cancel
	s.wakeupDone = make(chan struct{})
	config := s.Pool.Config().ConnConfig.Copy()
	go func() {
		defer close(s.wakeupDone)
		for ctx.Err() == nil {
			conn, e := pgx.ConnectConfig(ctx, config)
			if e == nil {
				_, e = conn.Exec(ctx, `LISTEN notif_work; LISTEN notif_events; LISTEN notif_provider`)
			}
			if e == nil {
				for ctx.Err() == nil {
					message, err := conn.WaitForNotification(ctx)
					if err != nil {
						break
					}
					if message.Channel == "notif_events" {
						s.eventWakes.signal(message.Payload)
						continue
					}
					if message.Channel == "notif_provider" {
						s.providerWakes.signal("")
						continue
					}
					var target []string
					if json.Unmarshal([]byte(message.Payload), &target) == nil && len(target) == 2 {
						s.wakes.signal(target[0] + "/" + target[1])
					}
				}
			}
			if conn != nil {
				_ = conn.Close(context.Background())
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
}
