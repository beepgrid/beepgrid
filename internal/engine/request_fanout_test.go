package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestRequestAudienceBatchPreservesPrivateEventsAndBoundsQueries(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	workers := []Actor{}
	ids := []string{}
	for i := range 32 {
		w := testWorker(t, s, owner, fmt.Sprint("request-fanout-", i))
		workers = append(workers, w)
		ids = append(ids, w.ID)
	}
	g, e := s.SaveGroup(ctx, owner, Group{Name: "Audience", Members: ids})
	if e != nil {
		t.Fatal(e)
	}
	q := featureCounter(t, s)
	q.total.Store(0)
	r, e := s.Ask(ctx, owner, AskInput{Target: Target{"group", g.ID}, Title: "Which release blockers remain?"}, "bounded-question")
	if e != nil {
		t.Fatal(e)
	}
	if n := q.total.Load(); n > 22 {
		t.Fatal("request fanout repeats writes per recipient", n)
	}
	views := map[string]string{}
	for _, a := range workers {
		x, e := s.Request(ctx, a, r.ID)
		if e != nil || x.ConversationID == r.ConversationID || views[x.ConversationID] != "" {
			t.Fatal("private request projection changed", x, e)
		}
		views[x.ConversationID] = a.ID
		events, _, e := s.EventPage(ctx, a, 0, false)
		if e != nil {
			t.Fatal(e)
		}
		seen := 0
		for _, ev := range events {
			if ev.Kind == "request.created" {
				seen++
				var body Request
				if json.Unmarshal(JSON(ev.Data), &body) != nil || body.ID != r.ID || body.ConversationID != x.ConversationID || ev.RecipientID != a.ID {
					t.Fatal("private event leaked", ev)
				}
			}
		}
		if seen != 1 {
			t.Fatal("private event count", seen)
		}
	}
	q.total.Store(0)
	if _, e = s.Respond(ctx, workers[0], r.ID, Response{Outcome: "answered", Answer: "All blockers cleared", Version: 1}); e != nil {
		t.Fatal(e)
	}
	if n := q.total.Load(); n > 30 {
		t.Fatal("decision events repeat writes per audience", n)
	}
	rows, e := s.Pool.Query(ctx, `SELECT sequence,recipient_id,conversation_id,data FROM events WHERE scope=$1 AND kind='request.resolved' ORDER BY sequence`, owner.Scope)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	count := 0
	var prior int64
	for rows.Next() {
		var seq int64
		var pid, conv string
		var raw []byte
		if e = rows.Scan(&seq, &pid, &conv, &raw); e != nil {
			t.Fatal(e)
		}
		var body Request
		if json.Unmarshal(raw, &body) != nil || body.ID != r.ID || body.State != "resolved" || body.ConversationID != conv {
			t.Fatal("event payload changed", string(raw))
		}
		if count == 0 {
			if pid != owner.ID || views[conv] != workers[0].ID {
				t.Fatal("requester event missing")
			}
		} else if views[conv] != pid {
			t.Fatal("recipient projection changed", pid, conv)
		}
		if count > 0 && seq != prior+1 {
			t.Fatal("event ordering gap", seq, prior)
		}
		prior = seq
		count++
	}
	if rows.Err() != nil || count != 33 {
		t.Fatal("event count", count, rows.Err())
	}
}

func TestRequestAudienceBatchRollsBackWhenOneRouteIsUnavailable(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	sender := testPerson(t, s, owner, "restricted-sender")
	first := testPerson(t, s, owner, "routed")
	second := testPerson(t, s, owner, "unrouted")
	g, e := s.SaveGroup(ctx, owner, Group{Name: "Audience", Members: []string{first.ID, second.ID}})
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.SaveConnection(ctx, owner, Connection{Name: "Provider", Kind: "email", Enabled: true, Config: map[string]string{"smtp_addr": "localhost:1025", "from": "from@example.com"}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: first.ID, Address: "first@example.com"}); e != nil {
		t.Fatal(e)
	}
	grant(t, s, owner, sender, []Rule{{Action: "ask", TargetKind: "group", TargetID: g.ID, ConnectionID: c.ID}})
	var before int64
	if e = s.Pool.QueryRow(ctx, `SELECT sequence FROM scopes WHERE id=$1`, owner.Scope).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Ask(ctx, sender, AskInput{Target: Target{"group", g.ID}, Title: "Deploy v2?"}, "partial-route"); ErrorStatus(e) != 403 {
		t.Fatal("missing route accepted", e)
	}
	var requests, conversations, deliveries int
	var sequence int64
	if e = s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM requests),(SELECT count(*) FROM conversations),(SELECT count(*) FROM deliveries),sequence FROM scopes WHERE id=$1`, owner.Scope).Scan(&requests, &conversations, &deliveries, &sequence); e != nil || requests != 0 || conversations != 0 || deliveries != 0 || sequence != before {
		t.Fatal("failed request left partial durable state", requests, conversations, deliveries, sequence, before, e)
	}
}
