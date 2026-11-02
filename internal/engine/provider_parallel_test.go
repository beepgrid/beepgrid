package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderLanesAcrossReplicasPreserveOrderAndExactAttempts(t *testing.T) {
	s, owner := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	schema := strings.TrimSuffix(s.Pool.Config().ConnConfig.RuntimeParams["search_path"], ",public")
	replica, err := OpenExisting(ctx, os.Getenv("BEEPGRID_TEST_DATABASE_URL"), schema, s.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	defer replica.Close()
	replica.AllowLocal = true
	var mu sync.Mutex
	active := map[string]bool{}
	seen := map[string]int{}
	var inFlight, peak atomic.Int32
	var overlaps, outOfOrder atomic.Bool
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Message struct {
				Body string `json:"body"`
			} `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		lane := strings.Split(body.Message.Body, "/")[0]
		mu.Lock()
		if active[lane] {
			overlaps.Store(true)
		}
		active[lane] = true
		if body.Message.Body != fmt.Sprintf("%s/%d", lane, seen[lane]) {
			outOfOrder.Store(true)
		}
		seen[lane]++
		mu.Unlock()
		n := inFlight.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(45 * time.Millisecond)
		inFlight.Add(-1)
		mu.Lock()
		active[lane] = false
		mu.Unlock()
		w.WriteHeader(202)
	}))
	defer receiver.Close()
	c, err := s.SaveConnection(ctx, owner, Connection{Name: "parallel fixture", Kind: "webhook", Enabled: true, Secret: "test-provider-secret", Config: map[string]string{"url": receiver.URL}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 25 {
		p := testPerson(t, s, owner, fmt.Sprint("lane-", i))
		if _, err = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: p.ID, Address: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
		for j := range 2 {
			body := fmt.Sprintf("%d/%d", i, j)
			if _, err = s.Send(ctx, owner, SendInput{Target: Target{"person", p.ID}, Body: body}, body); err != nil {
				t.Fatal(err)
			}
		}
	}
	var wg sync.WaitGroup
	for _, store := range []*Store{s, replica} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.DispatchBatch(ctx, "http://localhost", 100); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	// A replica may skip a workspace while the other claims it. Drain any
	// remaining ready work, with the same durable lane exclusion.
	if _, err = s.DispatchBatch(ctx, "http://localhost", 100); err != nil {
		t.Fatal(err)
	}
	if peak.Load() < 2 || overlaps.Load() || outOfOrder.Load() {
		t.Fatal("lane concurrency/order", peak.Load(), overlaps.Load(), outOfOrder.Load())
	}
	for i := range 25 {
		if seen[fmt.Sprint(i)] != 2 {
			t.Fatal("missing/duplicate send", i, seen)
		}
	}
	var accepted, attempts int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE connection_id=$1 AND state='provider_accepted'`, c.ID).Scan(&accepted); err != nil {
		t.Fatal(err)
	}
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM delivery_attempts WHERE delivery_id IN (SELECT id FROM deliveries WHERE connection_id=$1)`, c.ID).Scan(&attempts); err != nil || accepted != 50 || attempts != 50 {
		t.Fatal(accepted, attempts, err)
	}
}

func TestProviderLaneRechecksConnectionAfterInFlightSend(t *testing.T) {
	s, owner := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		w.WriteHeader(202)
	}))
	defer receiver.Close()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	c, err := s.SaveConnection(ctx, owner, Connection{Name: "revoke lane", Kind: "webhook", Enabled: true, Secret: "test-provider-secret", Config: map[string]string{"url": receiver.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: owner.ID, Address: "one-lane"}); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, err = s.Send(ctx, owner, SendInput{Target: Target{"person", owner.ID}, Body: fmt.Sprint(i)}, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() { _, err := s.DispatchBatch(ctx, "http://localhost", 32); done <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("send did not start")
	}
	c.Enabled = false
	if _, err = s.SaveConnection(ctx, owner, c); err != nil {
		t.Fatal(err)
	}
	unblock()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	var cancelled int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE connection_id=$1 AND state='cancelled'`, c.ID).Scan(&cancelled); err != nil || calls.Load() != 1 || cancelled != 1 {
		t.Fatal(calls.Load(), cancelled, err)
	}
}

func TestProviderPoolAcceptsLateProducersWhileSlowSendIsInFlight(t *testing.T) {
	s, owner := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	started, release, progress := make(chan struct{}), make(chan struct{}), make(chan struct{}, 25)
	var calls, active, peak atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := active.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		if calls.Add(1) == 1 {
			close(started)
			<-release
		} else {
			time.Sleep(15 * time.Millisecond)
			progress <- struct{}{}
		}
		active.Add(-1)
		w.WriteHeader(202)
	}))
	defer receiver.Close()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	c, err := s.SaveConnection(ctx, owner, Connection{Name: "late producers", Kind: "webhook", Enabled: true, Secret: "test-provider-secret", Config: map[string]string{"url": receiver.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: owner.ID, Address: "blocked"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, owner, SendInput{Target: Target{"person", owner.ID}, Body: "blocked"}, "late-first"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.DispatchBatch(ctx, "http://localhost", 64); done <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first send did not start")
	}
	for i := range 25 {
		p := testPerson(t, s, owner, fmt.Sprint("late-", i))
		if _, err = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: p.ID, Address: p.ID}); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Send(ctx, owner, SendInput{Target: Target{"person", p.ID}, Body: "later independent send"}, fmt.Sprint("later-", i)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-progress:
	case <-time.After(2 * time.Second):
		t.Fatal("late producers blocked behind unrelated slow destination")
	}
	unblock()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 26 || peak.Load() < 2 || peak.Load() > 16 {
		t.Fatal("pool loss, duplication or bound violation", calls.Load(), peak.Load())
	}
}
