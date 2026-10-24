package engine

import (
	"context"
	"testing"
)

func TestRolePreviewIsBoundedNonSavingAndRetainsOtherRoles(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "lead")
	p := testPerson(t, s, owner, "member")
	outside := testPerson(t, s, owner, "outside")
	agent := testWorker(t, s, owner, "worker")
	role := grant(t, s, owner, p, []Rule{{Action: "message", TargetKind: "agent", TargetID: agent.ID}})
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "role", TargetID: role.ID}, {Action: "manage", TargetKind: "role_binding", TargetID: role.ID}, {Action: "manage", TargetKind: "person", TargetID: p.ID}})
	proposed := role
	proposed.Rules = []Rule{}
	check := PolicyCheck{SubjectID: p.ID, Action: "message", Target: Target{"agent", agent.ID}}
	preview, e := s.PreviewRole(ctx, lead, RolePreviewInput{Role: proposed, Checks: []PolicyCheck{check}})
	if e != nil || !preview.CanSave || preview.Saves || len(preview.Affected) != 1 || !preview.Checks[0].Before || preview.Checks[0].After {
		t.Fatal(preview, e)
	}
	current, e := s.ExplainSubject(ctx, owner, p.ID, "message", check.Target, "", "")
	if e != nil || current["allowed"] != true {
		t.Fatal("preview changed a role", current, e)
	}
	grant(t, s, owner, p, role.Rules)
	preview, e = s.PreviewRole(ctx, lead, RolePreviewInput{Role: proposed, Checks: []PolicyCheck{check}})
	if e != nil || !preview.Checks[0].After {
		t.Fatal("removed another role's grant", preview, e)
	}
	proposed.Rules = []Rule{{Action: "decide", TargetKind: "agent", TargetID: "*"}}
	if _, e = s.PreviewRole(ctx, lead, RolePreviewInput{Role: proposed}); ErrorStatus(e) != 403 {
		t.Fatal("preview exceeded editor ceiling", e)
	}
	proposed.Rules = []Rule{}
	if e = s.BindRole(ctx, owner, Binding{role.ID, "principal", outside.ID}, false); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PreviewRole(ctx, lead, RolePreviewInput{Role: proposed}); ErrorStatus(e) != 403 {
		t.Fatal("shared outside assignment exposed", e)
	}
	if _, e = s.PreviewRole(ctx, outside, RolePreviewInput{Role: proposed}); ErrorStatus(e) != 403 {
		t.Fatal(e)
	}
}
