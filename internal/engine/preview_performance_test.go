package engine

import (
	"context"
	"fmt"
	"testing"
)

func TestRoleAndDispatchPreviewsBoundRepeatedPolicyAndRouteReads(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	operator := testPerson(t, s, owner, "operator")
	ids := []string{}
	var subject Actor
	for i := range 32 {
		w := testWorker(t, s, owner, fmt.Sprint("preview-", i))
		ids = append(ids, w.ID)
		if i == 0 {
			subject = w
		}
	}
	grant(t, s, owner, operator, []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}, {Action: "message", TargetKind: "agent", TargetID: "*"}})
	g, e := s.SaveGroup(ctx, owner, Group{Name: "Preview fleet", Members: ids})
	if e != nil {
		t.Fatal(e)
	}
	role := grant(t, s, owner, subject, []Rule{{Action: "view", TargetKind: "person", TargetID: owner.ID}})
	checks := []PolicyCheck{}
	for i := range 100 {
		checks = append(checks, PolicyCheck{SubjectID: subject.ID, Action: "message", Target: Target{"agent", ids[i%len(ids)]}})
	}
	q := featureCounter(t, s)
	q.total.Store(0)
	preview, e := s.PreviewRole(ctx, owner, RolePreviewInput{Role: role, Checks: checks})
	if e != nil || len(preview.Checks) != 100 || !preview.CanSave || preview.Saves {
		t.Fatal(preview, e)
	}
	if n := q.total.Load(); n > 12 {
		t.Fatal("role preview rereads the same subject for each cell", n)
	}
	for _, check := range preview.Checks {
		if !check.Before || !check.After {
			t.Fatal("other role grants subtracted", check)
		}
	}
	q.total.Store(0)
	dispatch, e := s.DispatchPreview(ctx, operator, DispatchPreviewInput{Targets: []Target{{"group", g.ID}}, Operation: "message"})
	if e != nil || dispatch.Count != 32 || len(dispatch.Recipients) != 32 || dispatch.Token == "" {
		t.Fatal(dispatch, e)
	}
	if n := q.total.Load(); n > 10 {
		t.Fatal("dispatch preview fetches routes per recipient", n)
	}
	var count int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&count); e != nil || count != 0 {
		t.Fatal("preview sent work", count, e)
	}
	if e = s.SetDisabled(ctx, owner, subject.ID, true); e != nil {
		t.Fatal(e)
	}
	preview, e = s.PreviewRole(ctx, owner, RolePreviewInput{Role: role, Checks: checks})
	if e != nil {
		t.Fatal(e)
	}
	for _, check := range preview.Checks {
		if check.Before || check.After {
			t.Fatal("disabled subject retained simulated authority", check)
		}
	}
}

func TestAttemptHistoryIndexBoundsRetainedRetries(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, owner, "attempt-history")
	if _, e := s.Send(ctx, owner, SendInput{Target: Target{"agent", w.ID}, Body: "Retry evidence"}, "attempts"); e != nil {
		t.Fatal(e)
	}
	deliveries, e := s.Deliveries(ctx, owner)
	if e != nil || len(deliveries) != 1 {
		t.Fatal(deliveries, e)
	}
	if _, e = s.Pool.Exec(ctx, `INSERT INTO delivery_attempts(id,scope,delivery_id,generation,outcome,created_at) SELECT 'retained-'||lpad(n::text,5,'0'),$1,$2,n,'failed',to_timestamp(1700000000)+n*interval '1 microsecond' FROM generate_series(1,10000) n`, owner.Scope, deliveries[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `ANALYZE delivery_attempts`); e != nil {
		t.Fatal(e)
	}
	plan := measuredPlanRows(t, s, `SELECT id,generation,outcome FROM delivery_attempts WHERE scope=$1 AND delivery_id=$2 ORDER BY created_at,id LIMIT 11`, owner.Scope, deliveries[0].ID)
	if plan["delivery_attempts"] > 11 {
		t.Fatal("attempt page sorts all retained retries", plan)
	}
}
