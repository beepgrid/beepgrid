package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeFanoutAtomicPrivateReplayAndCurrentMembership(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	workers := []Actor{}
	ids := []string{}
	for i := 0; i < 8; i++ {
		w := testWorker(t, s, owner, fmt.Sprint("batch-", i))
		workers = append(workers, w)
		ids = append(ids, w.ID)
	}
	group, e := s.SaveGroup(ctx, owner, Group{Name: "Batch", Members: ids})
	if e != nil {
		t.Fatal(e)
	}
	sender := testPerson(t, s, owner, "sender")
	role := grant(t, s, owner, sender, []Rule{{Action: "message", TargetKind: "group", TargetID: group.ID}, {Action: "view", TargetKind: "group", TargetID: group.ID}})
	var before int64
	if e = s.Pool.QueryRow(ctx, `SELECT sequence FROM scopes WHERE id=$1`, owner.Scope).Scan(&before); e != nil {
		t.Fatal(e)
	}
	in := SendInput{Target: Target{"group", group.ID}, Body: "Private batched payload", Title: "Batch audit"}
	result, e := s.Send(ctx, sender, in, "batch-once")
	if e != nil || len(result.Messages) != 8 {
		t.Fatal(result, e)
	}
	var after int64
	if e = s.Pool.QueryRow(ctx, `SELECT sequence FROM scopes WHERE id=$1`, owner.Scope).Scan(&after); e != nil || after-before != 8 {
		t.Fatal("cursor range", before, after, e)
	}
	repeat, e := s.Send(ctx, sender, in, "batch-once")
	if e != nil || string(JSON(repeat)) != string(JSON(result)) {
		t.Fatal("idempotency changed", repeat, e)
	}
	seen := map[string]bool{}
	for _, w := range workers {
		jobs, e := s.Inbox(ctx, w)
		if e != nil || len(jobs) != 1 {
			t.Fatal(jobs, e)
		}
		conv := jobs[0].Payload["conversation_id"].(string)
		if seen[conv] {
			t.Fatal("shared private conversation")
		}
		seen[conv] = true
		c, e := s.Conversation(ctx, w, conv)
		if e != nil || len(c.Messages) != 1 || len(c.Participants) != 2 || c.Messages[0].SenderID != sender.ID {
			t.Fatal(c, e)
		}
		if e = s.Ack(ctx, w, jobs[0].ID, jobs[0].Generation); e != nil {
			t.Fatal(e)
		}
	}
	// No stale group grant can deliver a queued payload to a removed member.
	if _, e = s.Send(ctx, sender, in, "batch-current"); e != nil {
		t.Fatal(e)
	}
	group.Members = ids[1:]
	if _, e = s.SaveGroup(ctx, owner, group); e != nil {
		t.Fatal(e)
	}
	jobs, e := s.Inbox(ctx, workers[0])
	if e != nil || len(jobs) != 0 {
		t.Fatal("removed member received batch", jobs, e)
	}
	// A connection-specific contact rule is not native permission. Denial must
	// roll back conversations, sequence reservation and all audience deliveries.
	role.Rules = []Rule{{Action: "message", TargetKind: "group", TargetID: group.ID, ConnectionID: "only-external"}}
	if _, e = s.SaveRole(ctx, owner, role); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE scope=$1`, owner.Scope).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Send(ctx, sender, in, "batch-denied"); ErrorStatus(e) != 403 {
		t.Fatal("native route restriction bypass", e)
	}
	var nowCount int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE scope=$1`, owner.Scope).Scan(&nowCount); e != nil || count != nowCount {
		t.Fatal("partial send", count, nowCount, e)
	}
}

func TestGroupFanoutWithProviderKeepsExternalAndPrivateNativeRoutes(t *testing.T) {
	s, owner := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	person := testPerson(t, s, owner, "routed-person")
	worker := testWorker(t, s, owner, "routed-worker")
	var count atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
		}
		if strings.Contains(string(raw), "_policy") {
			t.Error("policy metadata leaked")
		}
		count.Add(1)
		w.WriteHeader(202)
	}))
	defer receiver.Close()
	connection, e := s.SaveConnection(ctx, owner, Connection{Name: "Mixed group", Kind: "webhook", Config: map[string]string{"url": receiver.URL}, Secret: "fixture-signing-secret", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, owner, Destination{ConnectionID: connection.ID, PrincipalID: person.ID, Address: "person-only"}); e != nil {
		t.Fatal(e)
	}
	group, e := s.SaveGroup(ctx, owner, Group{Name: "Mixed", Members: []string{person.ID, worker.ID}})
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.Send(ctx, worker, SendInput{Target: Target{"group", group.ID}, Body: "Mixed routes"}, "mixed")
	if e != nil || len(result.Messages) != 2 {
		t.Fatal(result, e)
	}
	for _, m := range result.Messages {
		if m.InstanceID != worker.InstanceID {
			t.Fatal("worker attribution lost", m)
		}
	}
	var native, external int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE connection_id=''),count(*) FILTER(WHERE connection_id<>'') FROM deliveries WHERE scope=$1`, owner.Scope).Scan(&native, &external); e != nil || native != 2 || external != 1 {
		t.Fatal(native, external, e)
	}
	if e = s.DispatchOnce(ctx, "http://localhost"); e != nil || count.Load() != 1 {
		t.Fatal("provider path bypassed", count.Load(), e)
	}
	// A pure native worker-origin group also retains its authenticated instance.
	if _, e = s.SaveConnection(ctx, owner, Connection{ID: connection.ID, Name: connection.Name, Kind: "webhook", Config: map[string]string{"url": receiver.URL}, Enabled: false}); e != nil {
		t.Fatal(e)
	}
	result, e = s.Send(ctx, worker, SendInput{Target: Target{"group", group.ID}, Body: "Native instance"}, "native-instance")
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range result.Messages {
		if m.InstanceID != worker.InstanceID {
			t.Fatal("batch instance attribution", m)
		}
	}
}

func TestBatchedProviderFanoutFiltersRoutesAndRollsBackAtomically(t *testing.T) {
	s, owner := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	people := []Actor{testPerson(t, s, owner, "route-one"), testPerson(t, s, owner, "route-two")}
	sender := testPerson(t, s, owner, "restricted-sender")
	connections := []Connection{}
	for i := range 3 {
		c, err := s.SaveConnection(ctx, owner, Connection{Name: fmt.Sprint("route-", i), Kind: "webhook", Enabled: i != 2, Secret: "test-provider-secret", Config: map[string]string{"url": "http://127.0.0.1:9"}})
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, c)
		for _, p := range people {
			mode := "primary"
			if i == 1 {
				mode = "copy"
			}
			if i == 2 {
				mode = "fallback"
			}
			if _, err = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: p.ID, Address: "fixture", Mode: mode}); err != nil {
				t.Fatal(err)
			}
		}
	}
	g, err := s.SaveGroup(ctx, owner, Group{Name: "restricted routes", Members: []string{people[0].ID, people[1].ID}})
	if err != nil {
		t.Fatal(err)
	}
	grant(t, s, owner, sender, []Rule{{Action: "message", TargetKind: "group", TargetID: g.ID, ConnectionID: connections[0].ID}})
	in := SendInput{Target: Target{"group", g.ID}, Body: "only permitted external routes"}
	out, err := s.Send(ctx, sender, in, "provider-fanout")
	if err != nil || len(out.Messages) != 2 {
		t.Fatal(out, err)
	}
	again, err := s.Send(ctx, sender, in, "provider-fanout")
	if err != nil || string(JSON(again)) != string(JSON(out)) {
		t.Fatal("replay changed receipts", err)
	}
	var total, allowed int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE connection_id=$1) FROM deliveries`, connections[0].ID).Scan(&total, &allowed); err != nil || total != 2 || allowed != 2 {
		t.Fatal("route authorization", total, allowed, err)
	}
	// One recipient with no permitted route must abort the complete send,
	// including conversations, provider queue rows and event cursors.
	if _, err = s.Pool.Exec(ctx, `DELETE FROM destinations WHERE principal_id=$1 AND connection_id=$2`, people[1].ID, connections[0].ID); err != nil {
		t.Fatal(err)
	}
	var before, after int64
	if err = s.Pool.QueryRow(ctx, `SELECT sequence FROM scopes WHERE id=$1`, owner.Scope).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, sender, in, "provider-fanout-denied"); ErrorStatus(err) != 403 {
		t.Fatal("partial route denial accepted", err)
	}
	if err = s.Pool.QueryRow(ctx, `SELECT sequence FROM scopes WHERE id=$1`, owner.Scope).Scan(&after); err != nil || before != after {
		t.Fatal("partial event cursor", before, after, err)
	}
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&total); err != nil || total != 2 {
		t.Fatal("partial private messages", total, err)
	}
}
