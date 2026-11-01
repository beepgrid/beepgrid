package engine

import (
	"context"
	"strings"
	"testing"
)

func TestBoundApprovalExplainsAndEnforcesSeparateExecutorScope(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	session, err := s.Enroll(ctx, owner, EnrollInput{Agent: "release-reviewer", Name: "Release Reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := s.Authenticate(ctx, session.Token)
	if err != nil {
		t.Fatal(err)
	}
	approver := testPerson(t, s, owner, "release-authority")
	grant(t, s, owner, worker, []Rule{
		{Action: "view", TargetKind: "person", TargetID: approver.ID},
		{Action: "ask", TargetKind: "person", TargetID: approver.ID},
	})
	grant(t, s, owner, approver, []Rule{
		{Action: "view", TargetKind: "agent", TargetID: worker.ID},
		{Action: "decide", TargetKind: "agent", TargetID: worker.ID, ActionName: "publish", Conditions: &RuleConditions{Resource: "component/ledger"}},
	})
	action := Action{Name: "publish", ExecutorID: worker.ID, Resource: "component/ledger", ResourceVersion: "digest-1", OperationKey: "publish/ledger/1"}
	in := AskInput{Target: Target{"person", approver.ID}, Kind: "approval", Body: "Publish the qualified component?", Action: &action}
	_, err = s.Ask(ctx, worker, in, "missing-executor-scope")
	if ErrorStatus(err) != 403 || !strings.Contains(err.Error(), "executor, action name and resource") {
		t.Fatal("missing execution scope should fail with an actionable explanation", err)
	}
	grant(t, s, owner, worker, []Rule{{Action: "ask", TargetKind: "agent", TargetID: worker.ID, ActionName: "publish", Conditions: &RuleConditions{Resource: "component/ledger"}}})
	request, err := s.Ask(ctx, worker, in, "scoped-executor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, approver, request.ID, Response{Outcome: "approved", Version: request.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Redeem(ctx, worker, request.ID, action); err != nil {
		t.Fatal(err)
	}
	changed := action
	changed.Resource = "component/pricing"
	in.Action = &changed
	if _, err = s.Ask(ctx, worker, in, "outside-executor-scope"); ErrorStatus(err) != 403 {
		t.Fatal("component scope was widened", err)
	}
}
