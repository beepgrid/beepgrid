package httpapi

import (
	"bufio"
	"context"
	"fmt"
	"github.com/notif-dev/notif/internal/engine"
	"github.com/notif-dev/notif/sdk/go/notif"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEventStreamCommitWakeReplayAndMissedHintFallback(t *testing.T) {
	for _, summary := range []bool{false, true} {
		t.Run(fmt.Sprint("summary-", summary), func(t *testing.T) {
			s, c, server := setup(t)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			a, err := s.Authenticate(ctx, c.Token)
			if err != nil {
				t.Fatal(err)
			}
			var cursor int64
			if err = s.Pool.QueryRow(ctx, `SELECT sequence FROM scopes WHERE id=$1`, a.Scope).Scan(&cursor); err != nil {
				t.Fatal(err)
			}
			url := server.URL + "/api/v1/events?stream=1"
			if summary {
				url += "&summary=1"
			}
			req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
			req.Header.Set("Authorization", "Bearer "+c.Token)
			req.Header.Set("Last-Event-ID", fmt.Sprint(cursor))
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			reader := bufio.NewReader(res.Body)
			readMessage := func(want string) {
				t.Helper()
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(line, want) {
						return
					}
				}
			}
			readMessage(": keepalive")
			sent, err := s.Send(ctx, a, engine.SendInput{Target: engine.Target{Kind: "person", ID: a.ID}, Body: "wake the stream"}, "stream-wake")
			if err != nil {
				t.Fatal(err)
			}
			readMessage(sent.ConversationID)
			// Insert one durable event without a notification, as during a missed
			// hint or listener reconnect. Fallback must still find it.
			var seq int64
			if err = s.Pool.QueryRow(ctx, `WITH bumped AS (UPDATE scopes SET sequence=sequence+1 WHERE id=$1 RETURNING sequence) INSERT INTO events(scope,sequence,kind,actor_id,conversation_id,recipient_id,data) SELECT $1,sequence,'missed-hint',$2,$3,'','{}'::jsonb FROM bumped RETURNING sequence`, a.Scope, a.ID, sent.ConversationID).Scan(&seq); err != nil {
				t.Fatal(err)
			}
			readMessage("missed-hint")
			res.Body.Close()
			// Reconnect from the original cursor, retaining canonical IDs and
			// private history even if no notification is replayed.
			req, _ = http.NewRequestWithContext(ctx, "GET", url, nil)
			req.Header.Set("Authorization", "Bearer "+c.Token)
			req.Header.Set("Last-Event-ID", fmt.Sprint(cursor))
			res, err = http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			reader = bufio.NewReader(res.Body)
			readMessage(sent.ConversationID)
			readMessage("missed-hint")
		})
	}
}

func TestSlowEventReaderHasBoundedWriteAndDoesNotBlockOtherRequests(t *testing.T) {
	s, c, _ := setup(t)
	ctx := context.Background()
	a, e := s.Authenticate(ctx, c.Token)
	if e != nil {
		t.Fatal(e)
	}
	sent, e := s.Send(ctx, a, engine.SendInput{Target: engine.Target{Kind: "person", ID: a.ID}, Body: "stream fixture"}, "stream-fixture")
	if e != nil {
		t.Fatal(e)
	}
	var end int64
	if e = s.Pool.QueryRow(ctx, `UPDATE scopes SET sequence=sequence+250 WHERE id=$1 RETURNING sequence`, a.Scope).Scan(&end); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `INSERT INTO events(scope,sequence,kind,actor_id,conversation_id,recipient_id,data) SELECT $1,$2+n,'load.fixture',$3,$4,'',jsonb_build_object('body',repeat('x',32768)) FROM generate_series(1,250) n`, a.Scope, end-250, a.ID, sent.ConversationID); e != nil {
		t.Fatal(e)
	}
	closed := make(chan struct{}, 1)
	h := New(s, "http://localhost", nil)
	server := httptest.NewUnstartedServer(h)
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	server.Start()
	defer server.Close()
	conn, e := net.Dial("tcp", server.Listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	if e = conn.(*net.TCPConn).SetReadBuffer(1024); e != nil {
		t.Fatal(e)
	}
	fmt.Fprintf(conn, "GET /api/v1/events?stream=1 HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nConnection: close\r\n\r\n", server.Listener.Addr(), c.Token)
	// A second authenticated request must remain available while this peer never reads.
	other := notif.New(server.URL, c.Token)
	var me any
	if e = other.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); e != nil {
		t.Fatal(e)
	}
	select {
	case <-closed:
	case <-time.After(9 * time.Second):
		t.Fatal("non-reading peer retained an event handler")
	}
}
func TestOpenEventStreamRechecksRevokedCredential(t *testing.T) {
	s, c, server := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/events?stream=1", nil)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), "event-stream") {
		t.Fatal(res.StatusCode, res.Header)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE digest=$1`, engine.Digest(c.Token)); e != nil {
		t.Fatal(e)
	}
	started := time.Now()
	if _, e = io.ReadAll(res.Body); e != nil {
		t.Fatal(e)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("revoked stream remained open")
	}
}
