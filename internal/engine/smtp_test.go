package engine

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The fixture speaks the real protocol, including losing the final reply after
// storing the body. No SMTP client or delivery database method is mocked.
func smtpFixture(t *testing.T, mode func(int32) string) (Connection, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var attempts, accepted atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				m := mode(attempts.Add(1))
				if m == "greeting-lost" {
					return
				}
				r := bufio.NewReader(conn)
				if m == "greeting-stall" {
					_, _ = r.ReadString('\n')
					return
				}
				fmt.Fprint(conn, "220 localhost ESMTP\r\n")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL"):
						fmt.Fprint(conn, "250 OK\r\n")
					case strings.HasPrefix(line, "RCPT"):
						if strings.HasPrefix(m, "rcpt-") {
							fmt.Fprintf(conn, "%s private response body\r\n", strings.TrimPrefix(m, "rcpt-"))
							return
						}
						fmt.Fprint(conn, "250 OK\r\n")
					case strings.HasPrefix(line, "DATA"):
						if strings.HasPrefix(m, "data-") {
							fmt.Fprintf(conn, "%s private response body\r\n", strings.TrimPrefix(m, "data-"))
							return
						}
						fmt.Fprint(conn, "354 Continue\r\n")
						for {
							chunk, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if chunk == ".\r\n" {
								break
							}
						}
						if strings.HasPrefix(m, "final-") && m != "final-lost" {
							fmt.Fprintf(conn, "%s private response body\r\n", strings.TrimPrefix(m, "final-"))
							return
						}
						accepted.Add(1)
						if m == "final-lost" {
							return
						}
						fmt.Fprint(conn, "250 Stored\r\n")
					case strings.HasPrefix(line, "QUIT"):
						if m == "quit-lost" {
							return
						}
						if m == "quit-stall" {
							_, _ = r.ReadString('\n')
							return
						}
						fmt.Fprint(conn, "221 Bye\r\n")
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { l.Close(); wg.Wait() })
	return Connection{Kind: "email", Config: map[string]string{"smtp_addr": l.Addr().String(), "from": "sender@example.test"}}, &attempts, &accepted
}

func TestSMTPProtocolOutcomes(t *testing.T) {
	for _, tc := range []struct {
		mode, state string
		retry       bool
		accepted    int32
	}{
		{"healthy", "provider_accepted", false, 1},
		{"greeting-lost", "failed", true, 0},
		{"rcpt-451", "failed", true, 0}, {"rcpt-550", "failed", false, 0},
		{"data-451", "failed", true, 0}, {"data-550", "failed", false, 0},
		{"final-451", "failed", true, 0}, {"final-550", "failed", false, 0},
		{"final-lost", "unknown", false, 1},
		{"quit-lost", "provider_accepted", false, 1},
		{"quit-stall", "provider_accepted", false, 1},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			c, _, accepted := smtpFixture(t, func(int32) string { return tc.mode })
			s := &Store{AllowLocal: true}
			start := time.Now()
			r := s.sendMail(context.Background(), c, "receiver@example.test", "Body\r\n.\r\nmore body")
			if r.State != tc.state || r.Retry != tc.retry || accepted.Load() != tc.accepted || strings.Contains(r.Detail, "private response") {
				t.Fatal(r, accepted.Load())
			}
			if time.Since(start) > time.Second {
				t.Fatal("SMTP teardown blocked after a known outcome")
			}
		})
	}
}

func TestSMTPCancellationInterruptsNetworkWait(t *testing.T) {
	c, _, _ := smtpFixture(t, func(int32) string { return "greeting-stall" })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(40*time.Millisecond, cancel)
	start := time.Now()
	r := (&Store{AllowLocal: true}).sendMail(ctx, c, "receiver@example.test", "body")
	if time.Since(start) > time.Second || r.State != "failed" || !r.Retry {
		t.Fatal("cancellation/outcome mismatch", time.Since(start), r)
	}
}

func TestSMTPRetriesDefiniteFailureButNeverAmbiguousAcceptance(t *testing.T) {
	for _, mode := range []string{"final-451", "final-550", "final-lost", "quit-lost"} {
		t.Run(mode, func(t *testing.T) {
			s, owner := testStore(t)
			s.AllowLocal = true
			ctx := context.Background()
			c, attempts, accepted := smtpFixture(t, func(n int32) string {
				if n == 1 {
					return mode
				}
				return "healthy"
			})
			c.Name, c.Enabled = "phase fixture", true
			c, err := s.SaveConnection(ctx, owner, c)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: owner.ID, Address: "receiver@example.test"}); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Send(ctx, owner, SendInput{Target: Target{"person", owner.ID}, Body: "safe replay"}, "smtp-phase"); err != nil {
				t.Fatal(err)
			}
			if err = s.DispatchOnce(ctx, "http://localhost"); err != nil {
				t.Fatal(err)
			}
			var state string
			var next time.Time
			if err = s.Pool.QueryRow(ctx, `SELECT state,next_at FROM deliveries WHERE connection_id=$1`, c.ID).Scan(&state, &next); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"final-451": "queued", "final-550": "failed", "final-lost": "unknown", "quit-lost": "provider_accepted"}[mode]
			if state != want {
				t.Fatal(state, want)
			}
			if mode == "final-451" && time.Until(next) < time.Second {
				t.Fatal("unbounded immediate retry", next)
			}
			if _, err = s.Pool.Exec(ctx, `UPDATE deliveries SET next_at=now() WHERE connection_id=$1`, c.ID); err != nil {
				t.Fatal(err)
			}
			if err = s.DispatchOnce(ctx, "http://localhost"); err != nil {
				t.Fatal(err)
			}
			wantAttempts, wantAccepted := int32(1), int32(1)
			if mode == "final-451" {
				wantAttempts = 2
			}
			if mode == "final-550" {
				wantAccepted = 0
			}
			if attempts.Load() != wantAttempts || accepted.Load() != wantAccepted {
				t.Fatal(attempts.Load(), accepted.Load())
			}
		})
	}
}
