package engine

import (
	"context"
	"testing"
	"time"
)

func TestCapabilitiesMatchCurrentResponseAuthorization(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	agent := testWorker(t, s, owner, "billing")
	person := testPerson(t, s, owner, "approver")
	roles, e := s.Roles(ctx, owner)
	if e != nil {
		t.Fatal(e)
	}
	for _, role := range roles {
		if role.Name == "billing role" {
			if e = s.BindRole(ctx, owner, Binding{role.ID, "principal", agent.ID}, true); e != nil {
				t.Fatal(e)
			}
		}
	}
	group, e := s.SaveGroup(ctx, owner, Group{Name: "Finance", Members: []string{person.ID}})
	if e != nil {
		t.Fatal(e)
	}
	grant(t, s, owner, agent, []Rule{{Action: "ask", TargetKind: "agent", TargetID: "$self"}})
	proposerRole := grant(t, s, owner, agent, []Rule{{Action: "ask", TargetKind: "group", TargetID: group.ID}})
	grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "agent", TargetID: agent.ID}, {Action: "message", TargetKind: "agent", TargetID: agent.ID}})
	action := &Action{Name: "refund", ExecutorID: agent.ID, Resource: "invoice/1", ResourceVersion: "v1", OperationKey: "refund-1", Arguments: map[string]any{"amount_minor": int64(500), "currency": "USD"}}
	r, e := s.Ask(ctx, agent, AskInput{Target: Target{"group", group.ID}, Kind: "approval", Title: "Refund?", Action: action}, "caps-request")
	if e != nil {
		t.Fatal(e)
	}
	check := func(want bool) {
		t.Helper()
		c, e := s.Capabilities(ctx, person, CapabilityInput{RequestID: r.ID})
		if e != nil || c.Operations["respond"] != want || c.RequestVersion != r.Version {
			t.Fatalf("capabilities: %+v %v want %v", c, e, want)
		}
		if want && len(c.Outcomes) != 2 {
			t.Fatal(c)
		}
		if !want {
			_, e = s.Respond(ctx, person, r.ID, Response{Outcome: "approved", Version: r.Version})
			if ErrorStatus(e) != 403 {
				t.Fatal("mutation did not match denied capability", e)
			}
		}
	}
	check(false) // Message does not grant Decide.
	limit := int64(400)
	decision := grant(t, s, owner, person, []Rule{{Action: "decide", TargetKind: "agent", TargetID: agent.ID, ActionName: "refund", Conditions: &RuleConditions{Currency: "USD", MaxAmountMinor: &limit}}})
	check(false)
	decision.Rules[0].Conditions.MaxAmountMinor = nil
	if decision, e = s.SaveRole(ctx, owner, decision); e != nil {
		t.Fatal(e)
	}
	check(true)
	group.Members = []string{}
	if group, e = s.SaveGroup(ctx, owner, group); e != nil {
		t.Fatal(e)
	}
	check(false) // A durable audience is not permanent membership.
	group.Members = []string{person.ID}
	if group, e = s.SaveGroup(ctx, owner, group); e != nil {
		t.Fatal(e)
	}
	check(true)
	if e = s.BindRole(ctx, owner, Binding{proposerRole.ID, "principal", agent.ID}, true); e != nil {
		t.Fatal(e)
	}
	check(false) // Original caller's Ask is rechecked too.
	if e = s.BindRole(ctx, owner, Binding{proposerRole.ID, "principal", agent.ID}, false); e != nil {
		t.Fatal(e)
	}
	c, e := s.Capabilities(ctx, person, CapabilityInput{RequestID: r.ID})
	if e != nil || !c.Operations["respond"] {
		t.Fatal(c, e)
	}
	if _, e = s.Respond(ctx, person, r.ID, Response{Outcome: "approved", Version: r.Version}); e != nil {
		t.Fatal(e)
	}
	c, e = s.Capabilities(ctx, person, CapabilityInput{RequestID: r.ID})
	if e != nil || c.Operations["respond"] || len(c.Outcomes) != 0 {
		t.Fatal(c, e)
	}
	// Capabilities are observations, not transferable permission tickets.
	outside := testPerson(t, s, owner, "outside")
	if _, e = s.Capabilities(ctx, outside, CapabilityInput{RequestID: r.ID}); ErrorStatus(e) != 404 {
		t.Fatal(e)
	}
}
func TestScopedCapabilitiesAndAccessProvenance(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "lead")
	p := testPerson(t, s, owner, "member")
	g, e := s.SaveGroup(ctx, owner, Group{Name: "Team", Members: []string{p.ID}})
	if e != nil {
		t.Fatal(e)
	}
	role := grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "group", TargetID: g.ID}})
	c, e := s.Capabilities(ctx, lead, CapabilityInput{Target: Target{"person", p.ID}})
	if e != nil || !c.Operations["manage"] || c.Operations["message"] || c.Operations["issue_credential"] {
		t.Fatal(c, e)
	}
	if _, e = s.Access(ctx, lead, p.ID); ErrorStatus(e) != 404 {
		t.Fatal("identity manager inspected roles without access grant", e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "view", TargetKind: "access", TargetID: p.ID}})
	pr, e := s.SaveRole(ctx, owner, Role{Name: "Member", Rules: []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, owner, Binding{pr.ID, "group", g.ID}, false); e != nil {
		t.Fatal(e)
	}
	access, e := s.Access(ctx, lead, p.ID)
	if e != nil || len(access.Roles) != 1 || access.Roles[0].SubjectID != g.ID || access.Roles[0].SubjectKind != "group" {
		t.Fatal(access, e)
	}
	if _, e = s.ExplainSubject(ctx, lead, p.ID, "view", Target{"person", owner.ID}, "", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, owner, Binding{role.ID, "principal", lead.ID}, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Capabilities(ctx, lead, CapabilityInput{Target: Target{"person", p.ID}}); ErrorStatus(e) != 404 {
		t.Fatal("revoked manager", e)
	}
	// Expired requests cannot be acted on even with current grants.
	r, e := s.Ask(ctx, owner, AskInput{Target: Target{"person", p.ID}, Title: "Soon", ExpiresAt: time.Now().Add(time.Hour)}, "soon")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE requests SET expires_at=now()-interval '1 second' WHERE scope=$1 AND id=$2`, owner.Scope, r.ID); e != nil {
		t.Fatal(e)
	}
	c, e = s.Capabilities(ctx, owner, CapabilityInput{RequestID: r.ID})
	if e != nil || c.Operations["respond"] || c.Operations["cancel"] {
		t.Fatal(c, e)
	}
}
