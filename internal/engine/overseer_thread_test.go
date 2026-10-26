package engine

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

func TestOverseerPersonalFollowupsAndReplay(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "scout")
	alice := testPerson(t, s, owner, "alice")
	bob := testPerson(t, s, owner, "bob")
	rules := []Rule{{Action: "view", TargetKind: "agent", TargetID: worker.ID}}
	grant(t, s, owner, alice, rules)
	grant(t, s, owner, bob, rules)
	var calls atomic.Int32
	s.OverseerPlanner = func(ctx context.Context, prompt string, sources []FleetStatus) (OverseerDecision, error) {
		calls.Add(1)
		history, _ := ctx.Value(overseerHistoryKey{}).([]OverseerTurn)
		if prompt == "Explain that" && (len(history) != 1 || history[0].Prompt != "Alice's first question" || history[0].Dispatched) {
			t.Errorf("unexpected history: %+v", history)
		}
		return OverseerDecision{Operation: "status", Summary: prompt, AgentIDs: []string{worker.ID}}, nil
	}
	enableAssistance(t, s, owner, false, true)
	first, err := s.PlanOverseer(ctx, alice, OverseerInput{Prompt: "Alice's first question"}, "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.PlanOverseer(ctx, alice, OverseerInput{Prompt: "Alice's first question"}, "turn-1")
	if err != nil || again.ID != first.ID || calls.Load() != 1 {
		t.Fatal(again, err, calls.Load())
	}
	if _, err = s.PlanOverseer(ctx, alice, OverseerInput{Prompt: "Changed"}, "turn-1"); ErrorStatus(err) != 409 {
		t.Fatal("changed retry", err)
	}
	if _, err = s.PlanOverseer(ctx, bob, OverseerInput{Prompt: "Read Alice", PreviousPlanID: first.ID}); ErrorStatus(err) != 404 {
		t.Fatal("foreign context", err)
	}
	second, err := s.PlanOverseer(ctx, alice, OverseerInput{Prompt: "Explain that", PreviousPlanID: first.ID})
	if err != nil || second.PreviousPlanID != first.ID {
		t.Fatal(second, err)
	}
	bobPlan, err := s.PlanOverseer(ctx, bob, OverseerInput{Prompt: "Bob's own question"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Query(ctx, alice, QueryInput{Collection: "overseer_plans", Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].(OverseerPlan).ID != second.ID || page.NextCursor == "" || page.Order != "created_at_id_descending" {
		t.Fatal(page, err)
	}
	next, err := s.Query(ctx, alice, QueryInput{Collection: "overseer_plans", Limit: 1, Cursor: page.NextCursor})
	if err != nil || len(next.Items) != 1 || next.Items[0].(OverseerPlan).ID != first.ID {
		t.Fatal(next, err)
	}
	if _, err = s.Query(ctx, bob, QueryInput{Collection: "overseer_plans", Limit: 1, Cursor: page.NextCursor}); ErrorStatus(err) != 400 {
		t.Fatal("foreign cursor", err)
	}
	found, err := s.Query(ctx, bob, QueryInput{Collection: "overseer_plans", Search: "Bob's"})
	if err != nil || len(found.Items) != 1 || found.Items[0].(OverseerPlan).ID != bobPlan.ID {
		t.Fatal(found, err)
	}
	// A revoked ancestor invalidates its derived follow-up and any idempotency receipt.
	if _, err = s.Pool.Exec(ctx, `DELETE FROM role_bindings WHERE scope=$1 AND subject_id=$2`, owner.Scope, alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.OverseerPlan(ctx, alice, second.ID); ErrorStatus(err) != 404 {
		t.Fatal("derived leak", err)
	}
	if _, err = s.PlanOverseer(ctx, alice, OverseerInput{Prompt: "Alice's first question"}, "turn-1"); ErrorStatus(err) != 404 {
		t.Fatal("receipt leak", err)
	}
}

func TestOverseerBoundedHistoryRechecksDuringPlanning(t *testing.T) {
	for _, change := range []string{"six turn window", "revoked parent outside current group"} {
		t.Run(change, func(t *testing.T) {
			s, owner := testStore(t)
			ctx := context.Background()
			one := testWorker(t, s, owner, "one")
			two := testWorker(t, s, owner, "two")
			person := testPerson(t, s, owner, "operator")
			role := grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}})
			s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
				return OverseerDecision{Operation: "status", Summary: "Current reports"}, nil
			}
			enableAssistance(t, s, owner, false, true)
			first, err := s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Start"})
			if err != nil {
				t.Fatal(err)
			}
			previous := first.ID
			if change == "six turn window" {
				for i := 0; i < 9; i++ {
					p, e := s.PlanOverseer(ctx, person, OverseerInput{Prompt: fmt.Sprint(i), PreviousPlanID: previous})
					if e != nil {
						t.Fatal(e)
					}
					previous = p.ID
				}
				s.OverseerPlanner = func(ctx context.Context, _ string, _ []FleetStatus) (OverseerDecision, error) {
					h, _ := ctx.Value(overseerHistoryKey{}).([]OverseerTurn)
					if len(h) != 6 || h[0].Prompt != "3" || h[5].Prompt != "8" {
						t.Errorf("unbounded or unordered context: %+v", h)
					}
					return OverseerDecision{Operation: "status", Summary: "Bounded"}, nil
				}
			} else {
				group, e := s.SaveGroup(ctx, owner, Group{Name: "Smaller scope", Members: []string{one.ID}})
				if e != nil {
					t.Fatal(e)
				}
				grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "group", TargetID: group.ID}})
				s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
					role.Rules = []Rule{{Action: "view", TargetKind: "agent", TargetID: one.ID}}
					if _, e := s.SaveRole(ctx, owner, role); e != nil {
						t.Fatal(e)
					}
					return OverseerDecision{Operation: "status", Summary: "Must not survive revocation", AgentIDs: []string{one.ID}}, nil
				}
				if _, e = s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Follow up", PreviousPlanID: previous, Target: &Target{Kind: "group", ID: group.ID}}); ErrorStatus(e) != 404 {
					t.Fatal(two, e)
				}
				return
			}
			if _, err = s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Follow up", PreviousPlanID: previous}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOverseerDerivedHistoryRetainsGroupProvenance(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "scout")
	person := testPerson(t, s, owner, "alice")
	group, err := s.SaveGroup(ctx, owner, Group{Name: "Team", Members: []string{worker.ID}})
	if err != nil {
		t.Fatal(err)
	}
	grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "agent", TargetID: worker.ID}, {Action: "view", TargetKind: "group", TargetID: group.ID}})
	s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
		return OverseerDecision{Operation: "status", Summary: "Team report"}, nil
	}
	enableAssistance(t, s, owner, false, true)
	first, err := s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Team report", Target: &Target{Kind: "group", ID: group.ID}})
	if err != nil {
		t.Fatal(err)
	}
	follow, err := s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Explain", PreviousPlanID: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	group.Members = []string{}
	if _, err = s.SaveGroup(ctx, owner, group); err != nil {
		t.Fatal(err)
	}
	if _, err = s.OverseerPlan(ctx, person, follow.ID); ErrorStatus(err) != 404 {
		t.Fatal("ancestor group leaked", err)
	}
}

func TestOverseerConcurrentPlanningReceiptAndCallerBoundary(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "scout")
	s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
		return OverseerDecision{Operation: "status", Summary: "Current status"}, nil
	}
	enableAssistance(t, s, owner, false, true)
	if _, err := s.PlanOverseer(ctx, worker, OverseerInput{Prompt: "Use person context"}); ErrorStatus(err) != 403 {
		t.Fatal(err)
	}
	results := make(chan OverseerPlan, 2)
	failures := make(chan error, 2)
	for range 2 {
		go func() {
			p, e := s.PlanOverseer(ctx, owner, OverseerInput{Prompt: "Status"}, "concurrent-turn")
			results <- p
			failures <- e
		}()
	}
	first, second := <-results, <-results
	if e := <-failures; e != nil {
		t.Fatal(e)
	}
	if e := <-failures; e != nil {
		t.Fatal(e)
	}
	if first.ID != second.ID {
		t.Fatal("duplicate canonical turns", first.ID, second.ID)
	}
	var count int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM overseer_plans WHERE scope=$1`, owner.Scope).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
