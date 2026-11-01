package engine

import (
	"context"
	"reflect"
	"testing"
)

func TestReadPathsPreservePrivateProjectionsAndFreshPermissions(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "visible")
	two := testWorker(t, s, owner, "hidden")
	reader := testPerson(t, s, owner, "reader")
	group, e := s.SaveGroup(ctx, owner, Group{Name: "Visible agents", Members: []string{one.ID}})
	if e != nil {
		t.Fatal(e)
	}
	role := grant(t, s, owner, reader, []Rule{{Action: "view", TargetKind: "group", TargetID: group.ID}})
	// Private fan-out gives each audience member their own conversation projection.
	audience, e := s.SaveGroup(ctx, owner, Group{Name: "Readers", Members: []string{reader.ID, owner.ID}})
	if e != nil {
		t.Fatal(e)
	}
	q, e := s.Ask(ctx, one, AskInput{Target: Target{"group", audience.ID}, Title: "Private projection"}, "projection")
	if e != nil {
		t.Fatal(e)
	}
	hiddenView := grant(t, s, owner, reader, []Rule{{Action: "view", TargetKind: "agent", TargetID: two.ID}})
	if _, e = s.Ask(ctx, two, AskInput{Target: Target{"person", reader.ID}, Title: "Hidden requester"}, "hidden"); e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, owner, Binding{hiddenView.ID, "principal", reader.ID}, true); e != nil {
		t.Fatal(e)
	}
	list, e := s.Requests(ctx, reader)
	if e != nil || len(list) != 1 {
		t.Fatal(list, e)
	}
	single, e := s.Request(ctx, reader, q.ID)
	if e != nil || !reflect.DeepEqual(single, list[0]) || single.ConversationID == q.ConversationID {
		t.Fatal("private projection changed", single, list, e)
	}
	convs, e := s.Conversations(ctx, reader)
	if e != nil || len(convs) != 1 || convs[0].ID != single.ConversationID {
		t.Fatal(convs, e)
	}
	agents, e := s.Agents(ctx, reader)
	if e != nil || len(agents) != 1 || agents[0].ID != one.ID || len(agents[0].Instances) != 1 {
		t.Fatal(agents, e)
	}
	if e = s.Revoke(ctx, owner, one.CredentialID); e != nil {
		t.Fatal(e)
	}
	agents, e = s.Agents(ctx, reader)
	if e != nil || agents[0].Instances[0].Presence != "offline" {
		t.Fatal("revoked instance still connected", agents, e)
	}
	// A context reused by the caller must not preserve authority across operations.
	group.Members = []string{two.ID}
	if _, e = s.SaveGroup(ctx, owner, group); e != nil {
		t.Fatal(e)
	}
	list, e = s.Requests(ctx, reader)
	if e != nil || len(list) != 1 || list[0].Title != "Hidden requester" {
		t.Fatal("membership snapshot escaped operation", list, e)
	}
	agents, e = s.Agents(ctx, reader)
	if e != nil || len(agents) != 1 || agents[0].ID != two.ID {
		t.Fatal(agents, e)
	}
	if e = s.BindRole(ctx, owner, Binding{RoleID: role.ID, SubjectKind: "principal", SubjectID: reader.ID}, true); e != nil {
		t.Fatal(e)
	}
	list, e = s.Requests(ctx, reader)
	if e != nil || len(list) != 0 {
		t.Fatal("revoked role leaked requests", list, e)
	}
	convs, e = s.Conversations(ctx, reader)
	if e != nil || len(convs) != 0 {
		t.Fatal("revoked role leaked conversations", convs, e)
	}
	agents, e = s.Agents(ctx, reader)
	if e != nil || len(agents) != 0 {
		t.Fatal("hidden instance metadata", agents, e)
	}
}
