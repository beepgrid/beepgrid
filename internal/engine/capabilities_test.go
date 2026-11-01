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
	decision := grant(t, s, owner, person, []Rule{{Action: "decide", TargetKind: "agent", TargetID: agent.ID}})
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
	decision.Rules = nil
	if decision, e = s.SaveRole(ctx, owner, decision); e != nil {
		t.Fatal(e)
	}
	check(false) // Message does not grant Decide.
	limit := int64(400)
	decision = grant(t, s, owner, person, []Rule{{Action: "decide", TargetKind: "agent", TargetID: agent.ID, ActionName: "refund", Conditions: &RuleConditions{Currency: "USD", MaxAmountMinor: &limit}}})
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
	grant(t, s, owner, p, []Rule{{Action: "view", TargetKind: "person", TargetID: owner.ID}})
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

func TestDestinationCapabilitiesRequireAllDelegatedBounds(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "destination-cap-lead")
	member := testPerson(t, s, owner, "destination-cap-member")
	connection, err := s.SaveConnection(ctx, owner, Connection{Name: "Scoped hook", Kind: "webhook", Enabled: true, Config: map[string]string{"url": "https://example.com/hook"}, Secret: "test-only-signing-secret"})
	if err != nil {
		t.Fatal(err)
	}
	destination, err := s.SaveDestination(ctx, owner, Destination{ConnectionID: connection.ID, PrincipalID: member.ID, Address: "member", Mode: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	grant(t, s, owner, lead, []Rule{{Action: "view", TargetKind: "destination", TargetID: member.ID}, {Action: "manage", TargetKind: "connection", TargetID: connection.ID}})
	check := func(want bool) {
		t.Helper()
		cap, e := s.Capabilities(ctx, lead, CapabilityInput{Target: Target{"destination", destination.ID}})
		if e != nil || cap.Operations["manage"] != want {
			t.Fatal(cap, e, want)
		}
	}
	check(false)
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "person", TargetID: member.ID}})
	check(false)
	final := grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "destination", TargetID: member.ID}})
	check(true)
	changed, e := s.SaveDestination(ctx, owner, Destination{ID: destination.ID, Version: destination.Version, ConnectionID: connection.ID, PrincipalID: member.ID, Address: "new-address", Mode: "primary"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RemoveDestination(ctx, lead, destination.ID, destination.Version); ErrorStatus(e) != 409 {
		t.Fatal("stale destination removal accepted", e)
	}
	if changed.Version <= destination.Version {
		t.Fatal("revision did not advance")
	}
	if e := s.BindRole(ctx, owner, Binding{final.ID, "principal", lead.ID}, true); e != nil {
		t.Fatal(e)
	}
	check(false)
	outsider := testPerson(t, s, owner, "destination-cap-outsider")
	if _, e := s.Capabilities(ctx, outsider, CapabilityInput{Target: Target{"destination", destination.ID}}); ErrorStatus(e) != 404 {
		t.Fatal("hidden destination capability leaked", e)
	}
}

func TestAnswerCapabilitiesSeparatePublicationAndRecordManagement(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "answer-cap-worker")
	manager := testPerson(t, s, owner, "answer-cap-manager")
	request, err := s.Ask(ctx, worker, AskInput{Target: Target{"person", owner.ID}, Title: "Public information?", SharedKey: "answer-cap"}, "cap-answer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, owner, request.ID, Response{Outcome: "answered", Answer: "Yes", Version: request.Version}); err != nil {
		t.Fatal(err)
	}
	answer, err := s.PublishAnswer(ctx, owner, PublishInput{RequestID: request.ID, Audience: []string{worker.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Capabilities(ctx, manager, CapabilityInput{Target: Target{"answer", answer.ID}}); ErrorStatus(err) != 404 {
		t.Fatal(err)
	}
	role := grant(t, s, owner, manager, []Rule{{Action: "manage", TargetKind: "answer", TargetID: answer.ID}})
	cap, err := s.Capabilities(ctx, manager, CapabilityInput{Target: Target{"answer", answer.ID}})
	if err != nil || !cap.Operations["invalidate"] {
		t.Fatal(cap, err)
	}
	broad, err := s.Capabilities(ctx, manager, CapabilityInput{})
	if err != nil || broad.Operations["publish_answer"] {
		t.Fatal("record management became publication", broad, err)
	}
	if err = s.BindRole(ctx, owner, Binding{role.ID, "principal", manager.ID}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Capabilities(ctx, manager, CapabilityInput{Target: Target{"answer", answer.ID}}); ErrorStatus(err) != 404 {
		t.Fatal("revoked answer manager", err)
	}
}

func TestDeliveryCapabilitiesUseCurrentRoutePolicy(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	sender := testPerson(t, s, owner, "delivery-cap-sender")
	worker := testWorker(t, s, owner, "delivery-cap-worker")
	role := grant(t, s, owner, sender, []Rule{{Action: "message", TargetKind: "agent", TargetID: worker.ID}})
	sent, err := s.Send(ctx, sender, SendInput{Target: Target{"agent", worker.ID}, Body: "Delivery policy"}, "delivery-cap")
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Query(ctx, owner, QueryInput{Collection: "deliveries", Filters: map[string]string{"operation_id": sent.Messages[0].ID}})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	delivery := page.Items[0].(DeliveryEvidence).Delivery
	check := func(want bool) {
		t.Helper()
		c, e := s.Capabilities(ctx, owner, CapabilityInput{Target: Target{"delivery", delivery.ID}})
		if e != nil || c.Operations["retry"] != want {
			t.Fatal(c, e, want)
		}
	}
	check(false)
	if _, err = s.Pool.Exec(ctx, `UPDATE deliveries SET state='failed' WHERE scope=$1 AND id=$2`, owner.Scope, delivery.ID); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err = s.BindRole(ctx, owner, Binding{role.ID, "principal", sender.ID}, true); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err = s.RetryDelivery(ctx, owner, delivery.ID); ErrorStatus(err) != 403 {
		t.Fatal("retry ignored current source policy", err)
	}
	outsider := testPerson(t, s, owner, "delivery-cap-outside")
	if _, err = s.Capabilities(ctx, outsider, CapabilityInput{Target: Target{"delivery", delivery.ID}}); ErrorStatus(err) != 404 {
		t.Fatal("hidden delivery leaked", err)
	}
}
