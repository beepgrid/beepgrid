package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestWorkspaceTwentyReplicasRetainOneLogicalDelivery(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	first := testWorker(t, s, owner, "workspace/replicated")
	workers := []Actor{first}
	for range 19 {
		session, e := s.Enroll(ctx, owner, EnrollInput{Agent: "workspace/replicated", Name: "Replicated worker", Capabilities: []string{"message"}})
		if e != nil {
			t.Fatal(e)
		}
		actor, e := s.Authenticate(ctx, session.Token)
		if e != nil {
			t.Fatal(e)
		}
		if actor.ID != first.ID || actor.InstanceID == first.InstanceID {
			t.Fatal("stable identity or instance boundary lost")
		}
		workers = append(workers, actor)
	}
	instances, e := s.Instances(ctx, owner, first.ID)
	if e != nil || len(instances) != 20 {
		t.Fatal(len(instances), e)
	}
	if _, e = s.Send(ctx, owner, SendInput{Target: Target{"agent", first.ID}, Body: "One logical instruction"}, "logical"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	counts := make(chan int, 20)
	failures := make(chan error, 20)
	for _, worker := range workers {
		wg.Add(1)
		go func(a Actor) { defer wg.Done(); jobs, e := s.Inbox(ctx, a); counts <- len(jobs); failures <- e }(worker)
	}
	wg.Wait()
	close(counts)
	close(failures)
	total := 0
	for count := range counts {
		total += count
	}
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if total != 1 {
		t.Fatalf("logical delivery leased %d times across 20 replicas", total)
	}
}

func TestWorkspaceOverseerSeesOnlySevenOfOneHundredAgents(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	person := testPerson(t, s, owner, "bounded-fleet-observer")
	permitted := map[string]bool{}
	rules := []Rule{}
	for i := range 100 {
		session, e := s.Enroll(ctx, owner, EnrollInput{Agent: fmt.Sprintf("workspace/fleet-%03d", i), Name: fmt.Sprintf("Worker %03d", i)})
		if e != nil {
			t.Fatal(e)
		}
		if i < 7 {
			permitted[session.Agent.ID] = true
			rules = append(rules, Rule{Action: "view", TargetKind: "agent", TargetID: session.Agent.ID})
		}
	}
	grant(t, s, owner, person, rules)
	called := false
	s.OverseerPlanner = func(_ context.Context, _ string, sources []FleetStatus) (OverseerDecision, error) {
		called = true
		if len(sources) != 7 {
			t.Errorf("planner received %d sources", len(sources))
		}
		ids := []string{}
		for _, source := range sources {
			if !permitted[source.Agent.ID] {
				t.Error("unauthorized model context", source.Agent.ID)
			}
			ids = append(ids, source.Agent.ID)
		}
		return OverseerDecision{Operation: "status", AgentIDs: ids, Summary: "Status of the seven permitted agents."}, nil
	}
	enableAssistance(t, s, owner, true)
	plan, e := s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Show my fleet status"})
	if e != nil || !called || len(plan.Sources) != 7 || plan.RequiresConfirmation {
		t.Fatal(plan, e)
	}
	for _, source := range plan.Sources {
		if !permitted[source.Agent.ID] {
			t.Fatal("hidden source in saved plan")
		}
	}
	other := testPerson(t, s, owner, "other-fleet-observer")
	if _, e = s.OverseerPlan(ctx, other, plan.ID); ErrorStatus(e) != 404 {
		t.Fatal("foreign plan disclosed", e)
	}
}
