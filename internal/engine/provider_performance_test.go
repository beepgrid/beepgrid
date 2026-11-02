package engine

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderBatchReusesConnectionsAndPreservesAmbiguity(t *testing.T) {
	s, a := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	var connections, calls atomic.Int32
	var badSignature atomic.Bool
	receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !VerifySignature("local-provider-fixture", r.Header.Get("X-Beepgrid-Timestamp"), r.Header.Get("X-Beepgrid-Signature"), raw, time.Now()) {
			badSignature.Store(true)
		}
		index := calls.Add(1)
		// Real response delay, temporary rejection and an ambiguous response loss.
		time.Sleep(15 * time.Millisecond)
		switch index {
		case 2:
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(429)
		case 3:
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
		case 4:
			w.WriteHeader(400)
		default:
			w.WriteHeader(202)
		}
	}))
	receiver.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	receiver.Start()
	defer receiver.Close()
	c, e := s.SaveConnection(ctx, a, Connection{Name: "provider performance", Kind: "webhook", Enabled: true, Config: map[string]string{"url": receiver.URL}, Secret: "local-provider-fixture"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: a.ID, Address: "fixture"}); e != nil {
		t.Fatal(e)
	}
	for i := range 12 {
		if _, e = s.Send(ctx, a, SendInput{Target: Target{"person", a.ID}, Body: fmt.Sprint("batch ", i)}, fmt.Sprint("batch/", i)); e != nil {
			t.Fatal(e)
		}
	}
	n, e := s.DispatchBatch(ctx, "http://localhost", 32)
	if e != nil || n != 12 || calls.Load() != 12 || badSignature.Load() {
		t.Fatal("batch mismatch", n, calls.Load(), e)
	}
	if connections.Load() > 2 {
		t.Fatal("connections not reused", connections.Load())
	}
	var accepted, queued, unknown, failed, attempts int
	e = s.Pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='provider_accepted'),count(*) FILTER(WHERE state='queued'),count(*) FILTER(WHERE state='unknown'),count(*) FILTER(WHERE state='failed') FROM deliveries WHERE connection_id=$1`, c.ID).Scan(&accepted, &queued, &unknown, &failed)
	if e != nil || accepted != 9 || queued != 1 || unknown != 1 || failed != 1 {
		t.Fatal(accepted, queued, unknown, failed, e)
	}
	var next time.Time
	if e = s.Pool.QueryRow(ctx, `SELECT next_at FROM deliveries WHERE connection_id=$1 AND state='queued'`, c.ID).Scan(&next); e != nil || time.Until(next) < 2*time.Second {
		t.Fatal("retry deadline lost", next, e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE deliveries SET next_at=now() WHERE connection_id=$1 AND state='queued'`, c.ID); e != nil {
		t.Fatal(e)
	}
	if n, e = s.DispatchBatch(ctx, "http://localhost", 32); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM delivery_attempts WHERE delivery_id IN(SELECT id FROM deliveries WHERE connection_id=$1)`, c.ID).Scan(&attempts); e != nil || attempts != 13 || calls.Load() != 13 {
		t.Fatal("lost/duplicate attempt", attempts, calls.Load(), e)
	}
	// Definite/unknown failures do not silently re-enter the send queue.
	if n, e = s.DispatchBatch(ctx, "http://localhost", 32); e != nil || n != 0 || calls.Load() != 13 {
		t.Fatal(n, calls.Load(), e)
	}
}
