package engine

import (
	"context"
	"fmt"
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
