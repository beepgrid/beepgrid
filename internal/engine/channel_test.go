package engine

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
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
			w.Header().Set("Retry-After", "120")
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
	var next time.Time
	if e = s.Pool.QueryRow(ctx, `SELECT next_at FROM deliveries WHERE connection_id=$1`, c.ID).Scan(&next); e != nil || time.Until(next) < 119*time.Second {
		t.Fatal("provider Retry-After ignored", next, e)
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

func TestPrimaryFallbackAndUnknownOutcome(t *testing.T) {
	s, a := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	var primaryCount, fallbackCount atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { primaryCount.Add(1); w.WriteHeader(400) }))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fallbackCount.Add(1); w.WriteHeader(202) }))
	defer fallback.Close()
	for i, raw := range []string{primary.URL, fallback.URL} {
		c, e := s.SaveConnection(ctx, a, Connection{Name: fmt.Sprint("route-", i), Kind: "webhook", Config: map[string]string{"url": raw}, Enabled: true, Secret: "test-signing-secret"})
		if e != nil {
			t.Fatal(e)
		}
		mode := "primary"
		if i == 1 {
			mode = "fallback"
		}
		if _, e = s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: a.ID, Address: "alerts", Mode: mode}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.Send(ctx, a, SendInput{Target: Target{"person", a.ID}, Body: "Route test"}, "routing"); e != nil {
		t.Fatal(e)
	}
	var count int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE connection_id<>'' AND state='queued'`).Scan(&count)
	if count != 1 {
		t.Fatal("default sent everywhere", count)
	}
	if e := s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	if e := s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	if primaryCount.Load() != 1 || fallbackCount.Load() != 1 {
		t.Fatal(primaryCount.Load(), fallbackCount.Load())
	}
	// A lost provider response is unknown and must not silently fall back.
	unknown := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h := w.(http.Hijacker); c, _, _ := h.Hijack(); c.Close() }))
	defer unknown.Close()
	c, e := s.SaveConnection(ctx, a, Connection{Name: "Ambiguous", Kind: "webhook", Config: map[string]string{"url": unknown.URL}, Secret: "test-signing-secret", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: a.ID, Address: "alerts", Mode: "primary"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Send(ctx, a, SendInput{Target: Target{"person", a.ID}, Body: "Unknown"}, "unknown"); e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	if fallbackCount.Load() != 1 {
		t.Fatal("unknown outcome retried", fallbackCount.Load())
	}
	if retryAfter("120", time.Now()) != 120*time.Second || retryAfter("999999", time.Now()) != time.Hour {
		t.Fatal("retry-after bounds")
	}
}

func TestSMTPRelayWithoutInventedSigningSecret(t *testing.T) {
	s, owner := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	captured := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 localhost ESMTP\r\n")
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				fmt.Fprint(conn, "250 localhost\r\n")
			case strings.HasPrefix(line, "MAIL FROM"), strings.HasPrefix(line, "RCPT TO"):
				fmt.Fprint(conn, "250 OK\r\n")
			case strings.HasPrefix(line, "DATA"):
				fmt.Fprint(conn, "354 Continue\r\n")
				var message strings.Builder
				for {
					chunk, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if chunk == ".\r\n" {
						break
					}
					message.WriteString(chunk)
				}
				captured <- message.String()
				fmt.Fprint(conn, "250 Queued\r\n")
			case strings.HasPrefix(line, "QUIT"):
				fmt.Fprint(conn, "221 Bye\r\n")
				return
			default:
				fmt.Fprint(conn, "500 Unexpected command\r\n")
			}
		}
	}()
	c, e := s.SaveConnection(ctx, owner, Connection{Name: "Local mail", Kind: "email", Enabled: true, Config: map[string]string{"smtp_addr": listener.Addr().String(), "from": "notif@localhost.test"}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: owner.ID, Address: "developer@localhost.test"}); e != nil {
		t.Fatal(e)
	}
	worker := testWorker(t, s, owner, "email-worker")
	q, e := s.Ask(ctx, worker, AskInput{Target: Target{"person", owner.ID}, Title: "Email relay test"}, "smtp")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	select {
	case message := <-captured:
		if !strings.Contains(message, "/requests/"+q.ID) || !strings.Contains(message, "To: <developer@localhost.test>") {
			t.Fatal(message)
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP message missing")
	}
	if _, e = s.SaveConnection(ctx, owner, Connection{Name: "Missing password", Kind: "email", Enabled: true, Config: map[string]string{"smtp_addr": listener.Addr().String(), "from": "notif@localhost.test", "username": "sender"}}); ErrorStatus(e) != 400 {
		t.Fatal(e)
	}
	if _, e = s.SaveConnection(ctx, owner, Connection{Name: "Short valid SMTP password", Kind: "email", Enabled: true, Config: map[string]string{"smtp_addr": listener.Addr().String(), "from": "notif@localhost.test", "username": "sender"}, Secret: "p4ssw0rd"}); e != nil {
		t.Fatal(e)
	}
}
