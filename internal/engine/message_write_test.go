package engine

import (
	"context"
	"testing"
)

func TestBatchedSingleMessageKeepsInstanceReplayAndQuotaAtomicity(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	first := testWorker(t, s, owner, "replicated")
	session, err := s.Enroll(ctx, owner, EnrollInput{Agent: "replicated", Name: "replicated"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Authenticate(ctx, session.Token)
	if err != nil || first.ID != second.ID || first.InstanceID == second.InstanceID {
		t.Fatal("fixture", err)
	}
	in := SendInput{Target: Target{"instance", first.InstanceID}, Body: "Only the selected replica"}
	out, err := s.Send(ctx, owner, in, "single-replica")
	if err != nil || len(out.Messages) != 1 {
		t.Fatal(out, err)
	}
	replay, err := s.Send(ctx, owner, in, "single-replica")
	if err != nil || string(JSON(out)) != string(JSON(replay)) {
		t.Fatal("receipt changed", err)
	}
	if jobs, err := s.Inbox(ctx, second); err != nil || len(jobs) != 0 {
		t.Fatal("sibling received exact-instance message", jobs, err)
	}
	jobs, err := s.Inbox(ctx, first)
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	if err = s.Ack(ctx, first, jobs[0].ID, jobs[0].Generation); err != nil {
		t.Fatal(err)
	}
	reply, err := s.Send(ctx, first, SendInput{ConversationID: out.ConversationID, Body: "Replica attribution survives batching"}, "single-reply")
	if err != nil || reply.Messages[0].InstanceID != first.InstanceID {
		t.Fatal(reply, err)
	}
	// Quota failures must roll back every canonical write, cursor, delivery and receipt.
	if err = s.ApplyWorkspaceBudget(ctx, owner.Scope, WorkspaceBudget{Version: 1, Messages: 0, Requests: -1, UploadBytes: -1}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var raw string
		err := s.Pool.QueryRow(ctx, `SELECT json_build_array(sequence,(SELECT count(*) FROM messages),(SELECT count(*) FROM events),(SELECT count(*) FROM deliveries),(SELECT count(*) FROM conversations),(SELECT count(*) FROM idempotency))::text FROM scopes WHERE id=$1`, owner.Scope).Scan(&raw)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	for i, in := range []SendInput{{Target: Target{"agent", first.ID}, Body: "Must not create history"}, {ConversationID: out.ConversationID, Body: "Must not append history"}} {
		if _, err = s.Send(ctx, owner, in, []string{"quota-new", "quota-existing"}[i]); ErrorStatus(err) != 429 {
			t.Fatal("quota bypass", err)
		}
		if got := snapshot(); got != before {
			t.Fatal("partial quota mutation", before, got)
		}
	}
}

func TestIndexedRoleLookupKeepsDirectAndMultipleGroupBindings(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	viewer := testPerson(t, s, owner, "multiple-bindings")
	role := grant(t, s, owner, viewer, []Rule{{Action: "view", TargetKind: "person", TargetID: owner.ID}})
	groups := []Group{}
	for _, name := range []string{"one", "two"} {
		group, err := s.SaveGroup(ctx, owner, Group{Name: name, Members: []string{viewer.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if err = s.BindRole(ctx, owner, Binding{role.ID, "group", group.ID}, false); err != nil {
			t.Fatal(err)
		}
		groups = append(groups, group)
	}
	rules, err := roleRules(ctx, s.Pool, viewer)
	if err != nil || len(rules) != 3 {
		t.Fatal("direct or group binding lost", rules, err)
	}
	groups[0].Members = []string{}
	if _, err = s.SaveGroup(ctx, owner, groups[0]); err != nil {
		t.Fatal(err)
	}
	rules, err = roleRules(ctx, s.Pool, viewer)
	if err != nil || len(rules) != 2 {
		t.Fatal("membership change cached", rules, err)
	}
}
