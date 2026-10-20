package engine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEventHintsAcrossReplicasCommitRollbackAndCleanup(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	schema := strings.TrimSuffix(s.Pool.Config().ConnConfig.RuntimeParams["search_path"], ",public")
	other, err := OpenExisting(ctx, os.Getenv("NOTIF_TEST_DATABASE_URL"), schema, s.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	ready, release := other.WatchEvents(a)
	defer release()
	// Establish the remote listener before testing transaction boundaries.
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
wait:
	for {
		if _, err = s.Pool.Exec(ctx, `SELECT pg_notify('notif_events',$1)`, a.Scope); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ready:
			break wait
		case <-tick.C:
		case <-deadline:
			t.Fatal("replica listener did not start")
		}
	}
	next, drop := other.WatchEvents(a)
	defer drop()
	foreign, unwatch := other.WatchEvents(Actor{Scope: "unrelated-workspace"})
	defer unwatch()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = event(ctx, tx, a, "test.rollback", "", a.ID, map[string]string{"body": "private"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-next:
		t.Fatal("rollback signaled an event")
	case <-time.After(40 * time.Millisecond):
	}
	if _, err = s.Send(ctx, a, SendInput{Target: Target{"person", a.ID}, Body: "committed"}, "event-wake"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-next:
	case <-time.After(2 * time.Second):
		t.Fatal("replica missed commit hint")
	}
	select {
	case <-foreign:
		t.Fatal("cross-workspace hint")
	default:
	}
	// Slow/disconnected watchers never accumulate a per-event queue.
	for range 1000 {
		ch, stop := other.WatchEvents(a)
		other.eventWakes.signal(a.Scope)
		<-ch
		stop()
	}
	other.eventWakes.mu.Lock()
	defer other.eventWakes.mu.Unlock()
	if len(other.eventWakes.entries) != 1 {
		t.Fatal("watcher leak", len(other.eventWakes.entries))
	}
}

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
