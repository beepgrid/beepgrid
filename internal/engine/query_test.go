package engine

import (
	"context"
	"fmt"
	"testing"
)

func TestQueryTraversesBeyondHiddenWindowsAndBindsCursors(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	reader := testPerson(t, s, owner, "reader")
	// Deterministic keys put a permitted record beyond a full bounded denied scan.
	for i := range 507 {
		id := fmt.Sprintf("fixture_%04d", i)
		if _, e := s.Pool.Exec(ctx, `INSERT INTO principals(id,scope,kind,external_id,name) VALUES($1,$2,'agent',$1,$3)`, id, owner.Scope, fmt.Sprintf("Worker %d", i)); e != nil {
			t.Fatal(e)
		}
	}
	grant(t, s, owner, reader, []Rule{{Action: "view", TargetKind: "agent", TargetID: "fixture_0506"}})
	in := QueryInput{Collection: "agents", Limit: 3}
	first, e := s.Query(ctx, reader, in)
	if e != nil || len(first.Items) != 0 || first.NextCursor == "" {
		t.Fatal(first, e)
	}
	in.Cursor = first.NextCursor
	next, e := s.Query(ctx, reader, in)
	if e != nil || len(next.Items) != 1 || next.NextCursor != "" {
		t.Fatal(next, e)
	}
	if next.Items[0].(Principal).ID != "fixture_0506" {
		t.Fatal(next)
	}
	wrong := in
	wrong.Search = "other"
	if _, e = s.Query(ctx, reader, wrong); ErrorStatus(e) != 400 {
		t.Fatal("query rebinding", e)
	}
	other := testPerson(t, s, owner, "other")
	if _, e = s.Query(ctx, other, in); ErrorStatus(e) != 400 {
		t.Fatal("actor rebinding", e)
	}
	foreign, e := s.Bootstrap(ctx, "foreign/prod", "Foreign", "prod", Secret())
	if e != nil {
		t.Fatal(e)
	}
	foreign.ID = reader.ID
	if _, e = s.Query(ctx, foreign, in); ErrorStatus(e) != 400 {
		t.Fatal("tenant rebinding", e)
	}
	in.Cursor = in.Cursor[:len(in.Cursor)-4] + "aaaa"
	if _, e = s.Query(ctx, reader, in); ErrorStatus(e) != 400 {
		t.Fatal("tampered cursor", e)
	}
	// A role changed between pages cannot keep old read authority.
	roles, e := s.Roles(ctx, owner)
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range roles {
		if len(r.Rules) == 1 && r.Rules[0].TargetID == "fixture_0506" {
			if e = s.BindRole(ctx, owner, Binding{r.ID, "principal", reader.ID}, true); e != nil {
				t.Fatal(e)
			}
		}
	}
	in.Cursor = first.NextCursor
	next, e = s.Query(ctx, reader, in)
	if e != nil || len(next.Items) != 0 {
		t.Fatal(next, e)
	}
}
func TestQueryMessageHistoryAndFilters(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	person := testPerson(t, s, owner, "recipient")
	sent, e := s.Send(ctx, owner, SendInput{Target: Target{"person", person.ID}, Body: "Message 0"}, "first")
	if e != nil {
		t.Fatal(e)
	}
	for i := 1; i < 9; i++ {
		if _, e = s.Send(ctx, owner, SendInput{ConversationID: sent.ConversationID, Body: fmt.Sprintf("Message %d", i)}, fmt.Sprintf("m%d", i)); e != nil {
			t.Fatal(e)
		}
	}
	in := QueryInput{Collection: "messages", Limit: 2, Filters: map[string]string{"conversation_id": sent.ConversationID}}
	seen := map[string]bool{}
	n := 0
	for {
		page, e := s.Query(ctx, owner, in)
		if e != nil {
			t.Fatal(e)
		}
		for _, item := range page.Items {
			m := item.(Message)
			if seen[m.ID] || m.Body != fmt.Sprintf("Message %d", n) {
				t.Fatal("duplicate/order", m, n)
			}
			seen[m.ID] = true
			n++
		}
		if page.NextCursor == "" {
			break
		}
		in.Cursor = page.NextCursor
	}
	if n != 9 {
		t.Fatal(n)
	}
	if _, e = s.Query(ctx, person, QueryInput{Collection: "messages", Filters: in.Filters}); ErrorStatus(e) != 404 {
		t.Fatal("no sender View", e)
	}
	grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "person", TargetID: owner.ID}})
	page, e := s.Query(ctx, person, QueryInput{Collection: "messages", Search: "Message 7", Filters: in.Filters})
	if e != nil || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
	if _, e = s.Query(ctx, owner, QueryInput{Collection: "messages"}); ErrorStatus(e) != 400 {
		t.Fatal(e)
	}
	if _, e = s.Query(ctx, owner, QueryInput{Collection: "people", Filters: map[string]string{"unknown": "x"}}); ErrorStatus(e) != 400 {
		t.Fatal(e)
	}
	if _, e = s.Query(ctx, owner, QueryInput{Collection: "people", Limit: 201}); ErrorStatus(e) != 400 {
		t.Fatal(e)
	}
}
func TestQueryRedactsConnectionAndCredentialSecrets(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	person := testPerson(t, s, owner, "recipient")
	conn, e := s.SaveConnection(ctx, owner, Connection{Name: "Email", Kind: "email", Config: map[string]string{"from": "sender@example.com", "smtp_addr": "localhost:1025"}, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, owner, Destination{ConnectionID: conn.ID, PrincipalID: person.ID, Address: "recipient@example.com"}); e != nil {
		t.Fatal(e)
	}
	page, e := s.Query(ctx, person, QueryInput{Collection: "connections"})
	if e != nil || len(page.Items) != 1 || len(page.Items[0].(Connection).Config) != 0 || page.Items[0].(Connection).Secret != "" {
		t.Fatal(page, e)
	}
	page, e = s.Query(ctx, person, QueryInput{Collection: "credentials"})
	if e != nil || len(page.Items) != 1 || page.Items[0].(Credential).PrincipalID != person.ID {
		t.Fatal(page, e)
	}
}

func TestQueryFiltersCannotRevealHiddenMemberships(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	reader := testPerson(t, s, owner, "reader")
	p := testPerson(t, s, owner, "other")
	group, e := s.SaveGroup(ctx, owner, Group{Name: "Private team", Members: []string{p.ID}})
	if e != nil {
		t.Fatal(e)
	}
	grant(t, s, owner, reader, []Rule{{Action: "view", TargetKind: "person", TargetID: p.ID}})
	if _, e = s.Query(ctx, reader, QueryInput{Collection: "people", Filters: map[string]string{"group_id": group.ID}}); ErrorStatus(e) != 404 {
		t.Fatal("hidden group filter", e)
	}
	role := grant(t, s, owner, p, []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}})
	// Reading a definition does not imply reading everyone assigned to it.
	grant(t, s, owner, reader, []Rule{{Action: "view", TargetKind: "role", TargetID: role.ID}})
	page, e := s.Query(ctx, reader, QueryInput{Collection: "people", Filters: map[string]string{"role_id": role.ID}})
	if e != nil || len(page.Items) != 0 {
		t.Fatal(page, e)
	}
	grant(t, s, owner, reader, []Rule{{Action: "manage", TargetKind: "role_binding", TargetID: role.ID}, {Action: "manage", TargetKind: "person", TargetID: p.ID}})
	page, e = s.Query(ctx, reader, QueryInput{Collection: "people", Filters: map[string]string{"role_id": role.ID}})
	if e != nil || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
}

func TestAgentPresenceQueryUsesCurrentPermittedReplicas(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	reader := testPerson(t, s, owner, "presence-reader")
	visible, e := s.Enroll(ctx, owner, EnrollInput{Agent: "visible", Name: "Visible"})
	if e != nil {
		t.Fatal(e)
	}
	hidden, e := s.Enroll(ctx, owner, EnrollInput{Agent: "hidden", Name: "Hidden"})
	if e != nil {
		t.Fatal(e)
	}
	replica, e := s.Enroll(ctx, owner, EnrollInput{Agent: "visible", Name: "Visible"})
	if e != nil {
		t.Fatal(e)
	}
	grant(t, s, owner, reader, []Rule{{Action: "view", TargetKind: "agent", TargetID: visible.Agent.ID}})
	query := func(presence string, want int) {
		t.Helper()
		page, err := s.Query(ctx, reader, QueryInput{Collection: "agents", Filters: map[string]string{"presence": presence}, Limit: 50})
		if err != nil || len(page.Items) != want {
			t.Fatalf("%s: %#v %v", presence, page, err)
		}
		for _, item := range page.Items {
			if item.(Principal).ID == hidden.Agent.ID {
				t.Fatal("hidden identity leaked")
			}
		}
	}
	query("connected", 1)
	query("stale", 0)
	query("offline", 0)
	if _, e = s.Pool.Exec(ctx, `UPDATE instances SET last_seen=now()-interval '80 seconds' WHERE scope=$1 AND id=$2`, owner.Scope, visible.InstanceID); e != nil {
		t.Fatal(e)
	}
	query("connected", 1)
	if _, e = s.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE scope=$1 AND id=(SELECT credential_id FROM instances WHERE scope=$1 AND id=$2)`, owner.Scope, replica.InstanceID); e != nil {
		t.Fatal(e)
	}
	query("connected", 0)
	query("stale", 1)
	if _, e = s.Pool.Exec(ctx, `UPDATE principals SET disabled=true WHERE scope=$1 AND id=$2`, owner.Scope, visible.Agent.ID); e != nil {
		t.Fatal(e)
	}
	// Disabled identities remain discoverable to permitted directory readers,
	// while their runtime presence is offline, even if a heartbeat is recent.
	query("offline", 1)
	if _, e = s.Query(ctx, owner, QueryInput{Collection: "agents", Filters: map[string]string{"presence": "invented"}}); ErrorStatus(e) != 400 {
		t.Fatal(e)
	}
}
