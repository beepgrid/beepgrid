package engine

import (
	"context"
	"testing"
	"time"
)

func TestWakeupHintsPreserveInboxAuthority(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "notified")
	// Wait for a listener round-trip rather than assuming goroutine startup timing.
	ready, release := s.WatchWork(w)
	defer release()
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	listening := false
	for !listening {
		_ = notifyWork(ctx, s.Pool, a.Scope, w.ID)
		select {
		case <-ready:
			listening = true
		case <-deadline:
			t.Fatal("notification listener did not connect")
		case <-tick.C:
		}
	}
	next, drop := s.WatchWork(w)
	defer drop()
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = notifyWork(ctx, tx, a.Scope, w.ID); e != nil {
		t.Fatal(e)
	}
	if e = tx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case <-next:
		t.Fatal("rolled-back notification woke listener")
	case <-time.After(30 * time.Millisecond):
	}
	if _, e = s.Send(ctx, a, SendInput{Target: Target{"agent", w.ID}, Body: "Wake and read durable work"}, "wake"); e != nil {
		t.Fatal(e)
	}
	select {
	case <-next:
	case <-time.After(2 * time.Second):
		t.Fatal("committed message did not wake listener")
	}
	jobs, e := s.Inbox(ctx, w)
	if e != nil || len(jobs) != 1 {
		t.Fatal(jobs, e)
	}
}
