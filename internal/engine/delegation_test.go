package engine

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestDelegatedGroupManagementAndProtectedMemberships(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "lead")
	member := testPerson(t, s, owner, "member")
	outsider := testPerson(t, s, owner, "outsider")
	g, e := s.SaveGroup(ctx, owner, Group{Name: "Finance", Members: []string{lead.ID, member.ID}})
	if e != nil {
		t.Fatal(e)
	}
	other, e := s.SaveGroup(ctx, owner, Group{Name: "Engineering", Members: []string{outsider.ID}})
	if e != nil {
		t.Fatal(e)
	}
	groups, e := s.Groups(ctx, member)
	if e != nil || len(groups) != 1 || groups[0].ID != g.ID || len(groups[0].Members) != 1 || groups[0].Members[0] != member.ID {
		t.Fatalf("own group privacy: %+v %v", groups, e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "group", TargetID: g.ID}})
	g.Name = "Finance operations"
	if _, e = s.SaveGroup(ctx, lead, g); e != nil {
		t.Fatal("scoped rename", e)
	}
	if _, e = s.SaveGroup(ctx, lead, other); ErrorStatus(e) != 403 {
		t.Fatal("foreign group edit", e)
	}
	if _, e = s.SaveGroup(ctx, lead, Group{Name: "Global"}); ErrorStatus(e) != 403 {
		t.Fatal("collection creation", e)
	}
	g.Members = append(g.Members, outsider.ID)
	if _, e = s.SaveGroup(ctx, lead, g); ErrorStatus(e) != 403 {
		t.Fatal("outside member", e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "person", TargetID: outsider.ID}})
	if _, e = s.SaveGroup(ctx, lead, g); e != nil {
		t.Fatal("explicitly managed new member", e)
	}
	approver, e := s.SaveRole(ctx, owner, Role{Name: "Approver", Rules: []Rule{{Action: "decide", TargetKind: "agent", TargetID: "*"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, owner, Binding{approver.ID, "group", g.ID}, false); e != nil {
		t.Fatal(e)
	}
	g.Members = []string{lead.ID, member.ID}
	if _, e = s.SaveGroup(ctx, lead, g); ErrorStatus(e) != 403 {
		t.Fatal("protected role membership change", e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "role_binding", TargetID: approver.ID}})
	if _, e = s.SaveGroup(ctx, lead, g); e != nil {
		t.Fatal("explicit assignment authority", e)
	}
	if _, e = s.Send(ctx, lead, SendInput{Target: Target{"person", member.ID}, Body: "unauthorized"}, "manage-is-not-message"); ErrorStatus(e) != 403 {
		t.Fatal("management granted communication", e)
	}
}

func TestDelegatedRoleAssignmentsAndEditorCeiling(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "lead")
	member := testPerson(t, s, owner, "member")
	outside := testPerson(t, s, owner, "outside")
	g, e := s.SaveGroup(ctx, owner, Group{Name: "Team", Members: []string{member.ID}})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.SaveRole(ctx, owner, Role{Name: "Communicate", Rules: []Rule{{Action: "message", TargetKind: "agent", TargetID: "team/*"}}})
	if e != nil {
		t.Fatal(e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "role_binding", TargetID: r.ID}, {Action: "manage", TargetKind: "group", TargetID: g.ID}})
	if e = s.BindRole(ctx, lead, Binding{r.ID, "principal", member.ID}, false); e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, lead, Binding{r.ID, "principal", outside.ID}, false); ErrorStatus(e) != 403 {
		t.Fatal("outside subject", e)
	}
	if e = s.BindRole(ctx, lead, Binding{r.ID, "principal", owner.ID}, false); ErrorStatus(e) != 403 {
		t.Fatal("bootstrap subject", e)
	}
	powerful, e := s.SaveRole(ctx, owner, Role{Name: "Powerful", Rules: []Rule{{Action: "*", TargetKind: "*", TargetID: "*"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, lead, Binding{powerful.ID, "principal", member.ID}, false); ErrorStatus(e) != 403 {
		t.Fatal("unassigned role", e)
	}
	if _, e = s.SaveRole(ctx, lead, r); ErrorStatus(e) != 403 {
		t.Fatal("assign permission became edit permission", e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "role", TargetID: r.ID}, {Action: "message", TargetKind: "agent", TargetID: "team/*"}})
	r.Name = "Updated"
	if _, e = s.SaveRole(ctx, lead, r); e != nil {
		t.Fatal("bounded role edit", e)
	}
	r.Rules = []Rule{{Action: "decide", TargetKind: "agent", TargetID: "*"}}
	if _, e = s.SaveRole(ctx, lead, r); ErrorStatus(e) != 403 {
		t.Fatal("expanded rule", e)
	}
	r.Rules = []Rule{{Action: "message", TargetKind: "agent", TargetID: "team/*"}}
	if e = s.BindRole(ctx, owner, Binding{r.ID, "principal", outside.ID}, false); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveRole(ctx, lead, r); ErrorStatus(e) != 403 {
		t.Fatal("shared role outside managed boundary", e)
	}
	bindings, e := s.Bindings(ctx, lead)
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range bindings {
		if b.SubjectID == outside.ID || b.SubjectID == owner.ID {
			t.Fatalf("private binding exposed: %+v", b)
		}
	}
}

func TestDelegationRuleContainment(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	earlier := deadline.Add(-time.Minute)
	later := deadline.Add(time.Minute)
	limit := int64(10000)
	lower := int64(1000)
	higher := int64(20000)
	base := Rule{Action: "decide", TargetKind: "agent", TargetID: "billing", ConnectionID: "slack", ActionName: "refund", Conditions: &RuleConditions{Resource: "invoice", ResourceVersion: "v2", Currency: "USD", MaxAmountMinor: &limit, Before: &deadline}}
	for _, tc := range []struct {
		name   string
		change func(*Rule)
		want   bool
	}{
		{"same", func(r *Rule) {}, true},
		{"narrower", func(r *Rule) { r.Conditions.MaxAmountMinor = &lower; r.Conditions.Before = &earlier }, true},
		{"higher amount", func(r *Rule) { r.Conditions.MaxAmountMinor = &higher }, false},
		{"currency", func(r *Rule) { r.Conditions.Currency = "EUR" }, false},
		{"removed conditions", func(r *Rule) { r.Conditions = nil }, false},
		{"later", func(r *Rule) { r.Conditions.Before = &later }, false},
		{"all targets", func(r *Rule) { r.TargetID = "*" }, false},
		{"other action", func(r *Rule) { r.ActionName = "deploy" }, false},
		{"any connection", func(r *Rule) { r.ConnectionID = "" }, false},
		{"resource version", func(r *Rule) { r.Conditions.ResourceVersion = "v3" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := base
			c := *base.Conditions
			child.Conditions = &c
			tc.change(&child)
			if got := ruleContains(base, child, "lead", ""); got != tc.want {
				t.Fatalf("contained=%v want=%v", got, tc.want)
			}
		})
	}
	if ruleContains(Rule{Action: "view", TargetKind: "person", TargetID: "$self"}, Rule{Action: "view", TargetKind: "person", TargetID: "$self"}, "lead", "member") {
		t.Fatal("self rule expanded to another person's identity")
	}
	if ruleContains(Rule{Action: "view", TargetKind: "group", TargetID: "finance"}, Rule{Action: "view", TargetKind: "person", TargetID: "member"}, "lead", "") {
		t.Fatal("membership snapshot became permanent authority")
	}
}

func TestDelegatedAdministrationRevocationAndIsolation(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "lead")
	g, e := s.SaveGroup(ctx, owner, Group{Name: "Finance"})
	if e != nil {
		t.Fatal(e)
	}
	role := grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "group", TargetID: g.ID}})
	g.Name = "Allowed"
	if _, e = s.SaveGroup(ctx, lead, g); e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, owner, Binding{role.ID, "principal", lead.ID}, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveGroup(ctx, lead, g); ErrorStatus(e) != 403 {
		t.Fatal("stale actor retained grants", e)
	}
	foreign, e := s.Bootstrap(ctx, "other/production", "Other", "production", Secret())
	if e != nil {
		t.Fatal(e)
	}
	fg, e := s.SaveGroup(ctx, foreign, Group{Name: "Private"})
	if e != nil {
		t.Fatal(e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "group", TargetID: "*"}})
	if _, e = s.SaveGroup(ctx, lead, fg); ErrorStatus(e) != 403 {
		t.Fatal("cross-workspace group", e)
	}
	if _, e = s.Groups(ctx, lead); e != nil {
		t.Fatal(e)
	}
}

func TestConcurrentProtectedMembershipAndRoleAssignment(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "lead")
	p := testPerson(t, s, owner, "member")
	g, e := s.SaveGroup(ctx, owner, Group{Name: "Team"})
	if e != nil {
		t.Fatal(e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "group", TargetID: g.ID}, {Action: "manage", TargetKind: "person", TargetID: p.ID}})
	role, e := s.SaveRole(ctx, owner, Role{Name: "Protected", Rules: []Rule{{Action: "decide", TargetKind: "agent", TargetID: "*"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, owner, Binding{role.ID, "group", g.ID}, false); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy := g
			copy.Members = []string{p.ID}
			if _, err := s.SaveGroup(ctx, lead, copy); ErrorStatus(err) != 403 {
				t.Errorf("protected membership accepted: %v", err)
			}
		}()
	}
	wg.Wait()
	var n int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE scope=$1 AND group_id=$2`, owner.Scope, g.ID).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
}

func TestDelegatedCredentialAndAccountLinkCannotImpersonate(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "lead")
	member := testPerson(t, s, owner, "member")
	privileged := testPerson(t, s, owner, "approver")
	grant(t, s, owner, privileged, []Rule{{Action: "decide", TargetKind: "agent", TargetID: "billing"}})
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "person", TargetID: "*"}, {Action: "manage", TargetKind: "credential", TargetID: "*"}, {Action: "manage", TargetKind: "account", TargetID: "*"}})
	key, e := s.NewCredential(ctx, lead, member.ID, "api", "")
	if e != nil || key["token"] == "" {
		t.Fatal(e)
	}
	if _, e = s.NewCredential(ctx, lead, privileged.ID, "api", ""); ErrorStatus(e) != 403 {
		t.Fatal("privileged token impersonation", e)
	}
	if _, e = s.NewCredential(ctx, lead, owner.ID, "api", ""); ErrorStatus(e) != 403 {
		t.Fatal("owner token impersonation", e)
	}
	if e = s.LinkOIDC(ctx, lead, privileged.ID, "https://issuer.example", "attacker"); ErrorStatus(e) != 403 {
		t.Fatal("privileged OIDC takeover", e)
	}
	if e = s.LinkSlackAccount(ctx, lead, owner.ID, "attacker", "team"); ErrorStatus(e) != 403 {
		t.Fatal("owner Slack takeover", e)
	}
	if e = s.LinkSlackAccount(ctx, lead, member.ID, "U-member", "team"); e != nil {
		t.Fatal("bounded account mapping", e)
	}
	if _, e = s.UpdatePerson(ctx, lead, owner.ID, CreatePerson{Name: "Replaced"}); ErrorStatus(e) != 403 {
		t.Fatal("owner profile modified", e)
	}
	if e = s.SetDisabled(ctx, lead, owner.ID, true); ErrorStatus(e) != 403 {
		t.Fatal("owner disabled", e)
	}
	if e = s.Revoke(ctx, lead, key["id"].(string)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, key["token"].(string)); ErrorStatus(e) != 401 {
		t.Fatal("revoked token works", e)
	}
	if _, e = s.NewCredential(ctx, member, lead.ID, "api", ""); ErrorStatus(e) != 403 {
		t.Fatal("plain member minted identity", e)
	}
}

func TestDelegatedIntegrationsCannotRerouteUnmanagedDestinations(t *testing.T) {
	s, owner := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	lead := testPerson(t, s, owner, "lead")
	member := testPerson(t, s, owner, "member")
	other := testPerson(t, s, owner, "other")
	c, e := s.SaveConnection(ctx, owner, Connection{Name: "Mail", Kind: "email", Config: map[string]string{"smtp_addr": "127.0.0.1:1125", "from": "team@example.com"}, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: member.ID, Address: "member@example.com"})
	if e != nil {
		t.Fatal(e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "connection", TargetID: c.ID}, {Action: "manage", TargetKind: "destination", TargetID: member.ID}, {Action: "manage", TargetKind: "person", TargetID: member.ID}})
	c.Name = "Managed mail"
	if _, e = s.SaveConnection(ctx, lead, c); e != nil {
		t.Fatal(e)
	}
	d.Address = "updated@example.com"
	if _, e = s.SaveDestination(ctx, lead, d); e != nil {
		t.Fatal(e)
	}
	own, e := s.Connections(ctx, member)
	if e != nil || len(own) != 1 || len(own[0].Config) != 0 || own[0].Secret != "" {
		t.Fatal("recipient config disclosure", own, e)
	}
	if _, e = s.SaveDestination(ctx, lead, Destination{ConnectionID: c.ID, PrincipalID: other.ID, Address: "other@example.com"}); ErrorStatus(e) != 403 {
		t.Fatal("outside destination", e)
	}
	if _, e = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: other.ID, Address: "other@example.com"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveConnection(ctx, lead, c); ErrorStatus(e) != 403 {
		t.Fatal("shared outside connection was editable", e)
	}
	if e = s.RemoveDestination(ctx, lead, d.ID); e != nil {
		t.Fatal("own destination removal", e)
	}
	enabled := true
	if _, e = s.UpdateAssistance(ctx, lead, AssistanceUpdate{Overseer: &enabled}); ErrorStatus(e) != 403 {
		t.Fatal("integration manager enabled assistance", e)
	}
}

func TestManagementDoesNotExposeFleetProgressOrBusinessDecisions(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "lead")
	worker := testWorker(t, s, owner, "billing")
	if e := s.Heartbeat(ctx, worker, map[string]any{"private": "customer revenue"}); e != nil {
		t.Fatal(e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "agent", TargetID: worker.ID}})
	agents, e := s.Agents(ctx, lead)
	if e != nil || len(agents) != 1 || len(agents[0].Instances) != 0 {
		t.Fatalf("progress leak: %+v %v", agents, e)
	}
	status, e := s.FleetStatus(ctx, lead, nil)
	if e != nil || status["total"] != 0 {
		t.Fatal("fleet view authority leak", status, e)
	}
	if _, e = s.Instances(ctx, lead, worker.ID); ErrorStatus(e) != 403 {
		t.Fatal("instance visibility", e)
	}
	approval, e := s.Ask(ctx, worker, AskInput{Kind: "approval", Target: Target{"person", lead.ID}, Title: "Refund?", Action: &Action{Name: "refund", ExecutorID: worker.ID, Resource: "invoice", ResourceVersion: "1", OperationKey: "once"}}, "management-approval")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, lead, approval.ID, Response{Outcome: "approved", Version: approval.Version}); ErrorStatus(e) != 404 && ErrorStatus(e) != 403 {
		t.Fatal("manage became decide", e)
	}
}

func TestScopedAnswerAndAssistanceManagement(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "publisher")
	worker := testWorker(t, s, owner, "research")
	reviewer := testPerson(t, s, owner, "reviewer")
	grant(t, s, owner, lead, []Rule{{Action: "view", TargetKind: "agent", TargetID: worker.ID}, {Action: "message", TargetKind: "agent", TargetID: worker.ID}, {Action: "manage", TargetKind: "answer", TargetID: "$self"}})
	q, e := s.Ask(ctx, worker, AskInput{Target: Target{"person", lead.ID}, Title: "Currency?", SharedKey: "policy/v1"}, "publish")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, lead, q.ID, Response{Outcome: "answered", Answer: "USD", Version: q.Version}); e != nil {
		t.Fatal(e)
	}
	answer, e := s.PublishAnswer(ctx, lead, PublishInput{RequestID: q.ID, Audience: []string{worker.ID}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.InvalidateAnswer(ctx, reviewer, answer.ID); ErrorStatus(e) != 403 {
		t.Fatal("unmanaged answer invalidation", e)
	}
	grant(t, s, owner, reviewer, []Rule{{Action: "manage", TargetKind: "answer", TargetID: answer.ID}})
	answers, e := s.Answers(ctx, reviewer)
	if e != nil || len(answers) != 1 {
		t.Fatal(answers, e)
	}
	if e = s.InvalidateAnswer(ctx, reviewer, answer.ID); e != nil {
		t.Fatal(e)
	}
	disabled := false
	if _, e = s.UpdateAssistance(ctx, reviewer, AssistanceUpdate{Overseer: &disabled}); ErrorStatus(e) != 403 {
		t.Fatal("answer manager changed assistance", e)
	}
	grant(t, s, owner, reviewer, []Rule{{Action: "manage", TargetKind: "assistance", TargetID: "workspace"}})
	if _, e = s.UpdateAssistance(ctx, reviewer, AssistanceUpdate{Overseer: &disabled}); e != nil {
		t.Fatal(e)
	}
}
