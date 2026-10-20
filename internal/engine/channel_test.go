package engine

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSignedProviderRetriesAndEncryptedCredentials(t *testing.T) {
	s, a := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	secret := "a-long-private-signing-secret"
	var count atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !VerifySignature(secret, r.Header.Get("X-Notif-Timestamp"), r.Header.Get("X-Notif-Signature"), raw, time.Now()) {
			t.Error("unsigned provider delivery")
		}
		if strings.Contains(string(raw), "_policy") {
			t.Error("private policy payload leaked")
		}
		if count.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(202)
	}))
	defer receiver.Close()
	c, e := s.SaveConnection(ctx, a, Connection{Name: "Test webhook", Kind: "webhook", Config: map[string]string{"url": receiver.URL}, Secret: secret, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	var encrypted []byte
	if e = s.Pool.QueryRow(ctx, `SELECT secret FROM connections WHERE id=$1`, c.ID).Scan(&encrypted); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(encrypted), secret) || c.Secret != "" {
		t.Fatal("provider credential leaked")
	}
	if _, e = s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: a.ID, Address: "test"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Send(ctx, a, SendInput{Target: Target{"person", a.ID}, Body: "Hello"}, "provider"); e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	var state string
	if e = s.Pool.QueryRow(ctx, `SELECT state FROM deliveries WHERE connection_id=$1`, c.ID).Scan(&state); e != nil || state != "queued" {
		t.Fatal(state, e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE deliveries SET next_at=now() WHERE connection_id=$1`, c.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	if e = s.Pool.QueryRow(ctx, `SELECT state FROM deliveries WHERE connection_id=$1`, c.ID).Scan(&state); e != nil || state != "provider_accepted" || count.Load() != 2 {
		t.Fatal(state, count.Load(), e)
	}
}
func TestProviderSecurityBoundaries(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	if _, e := s.SaveConnection(ctx, a, Connection{Name: "Private", Kind: "webhook", Config: map[string]string{"url": "https://127.0.0.1/internal"}, Secret: "long-signing-secret", Enabled: true}); ErrorStatus(e) != 400 {
		t.Fatal("private URL accepted", e)
	}
	now := time.Now()
	ts := "1"
	if VerifySignature("secret", ts, Signature("secret", ts, []byte("x")), []byte("x"), now) {
		t.Fatal("old callback accepted")
	}
	if _, e := s.unseal([]byte("bad")); e == nil {
		t.Fatal("invalid ciphertext accepted")
	}
}
