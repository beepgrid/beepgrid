package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSlackThreadsRoundTripAndDuplicateEvents(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "slack-worker")
	p := testPerson(t, s, a, "slack-person")
	grant(t, s, a, p, []Rule{{Action: "view", TargetKind: "agent", TargetID: w.ID, ConnectionID: "", ActionName: ""}, {Action: "message", TargetKind: "agent", TargetID: w.ID, ConnectionID: "", ActionName: ""}})
	c, e := s.SaveConnection(ctx, a, Connection{Name: "Slack threads", Kind: "slack", Enabled: true, Config: map[string]string{"team_id": "T1", "default_agent_id": w.ID}, Secret: `{"bot_token":"xoxb-test-fixture","signing_secret":"long-secret-fixture"}`})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: p.ID, Address: "C1"}); e != nil {
		t.Fatal(e)
	}
	posted := []map[string]any{}
	s.providerTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		posted = append(posted, payload)
		if r.URL.String() != "https://slack.com/api/chat.postMessage" || r.Header.Get("Authorization") != "Bearer xoxb-test-fixture" {
			t.Error("incorrect Slack endpoint/auth")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"ts":"123.4","channel":"C1"}`)), Header: http.Header{}}, nil
	})
	first, e := s.Send(ctx, w, SendInput{Target: Target{"person", p.ID}, Body: "Hello Slack"}, "slack-first")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchOnce(ctx, "https://beepgrid.test"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Send(ctx, w, SendInput{ConversationID: first.ConversationID, Body: "Same thread"}, "slack-second"); e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchOnce(ctx, "https://beepgrid.test"); e != nil {
		t.Fatal(e)
	}
	if len(posted) != 2 || posted[1]["thread_ts"] != "123.4" {
		t.Fatal(posted)
	}
	person, e := s.SlackActor(ctx, a.Scope, "not-linked", "T1")
	if e == nil {
		t.Fatal("unknown Slack account trusted", person)
	}
	if e = s.LinkSlackAccount(ctx, a, p.ID, "U1", "T1"); e != nil {
		t.Fatal(e)
	}
	person, e = s.SlackActor(ctx, a.Scope, "U1", "T1")
	if e != nil {
		t.Fatal(e)
	}
	person.ProviderConnection = c.ID
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, e := s.SlackMessage(ctx, person, c, "C1", "123.4", "Human reply", "Ev1")
			if e != nil || result.ConversationID != first.ConversationID {
				t.Error(result, e)
			}
		}()
	}
	wg.Wait()
	var count int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE body='Human reply'`).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate Slack reply", count)
	}
	if _, e = s.SlackMessage(ctx, person, c, "C1", "123.4", "Different reply", "Ev1"); ErrorStatus(e) != 409 {
		t.Fatal("event ID mutation", e)
	}
	// New human-initiated threads are also atomic and retry the same canonical conversation.
	fresh, e := s.SlackMessage(ctx, person, c, "C2", "456.7", "Start new conversation", "Ev2")
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.SlackMessage(ctx, person, c, "C2", "456.7", "Start new conversation", "Ev2")
	if e != nil || again.ConversationID != fresh.ConversationID {
		t.Fatal("new-thread duplicate", e)
	}
	if _, e = s.Send(ctx, w, SendInput{ConversationID: fresh.ConversationID, Body: "Reply to originating thread"}, "origin-reply"); e != nil {
		t.Fatal(e)
	}
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE address='C2' AND connection_id=$1`, c.ID).Scan(&count)
	if count != 1 {
		t.Fatal("origin thread route missing", count)
	}
}
