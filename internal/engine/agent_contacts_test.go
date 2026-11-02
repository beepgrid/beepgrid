package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentContactsUsesBothViewersAndCurrentPolicy(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "Builder 7")
	person := testPerson(t, s, owner, "Maya")
	hidden := testPerson(t, s, owner, "Hidden person")
	viewer := testPerson(t, s, owner, "Team lead")
	// Remove the fixture's broad grant so the subject's permissions matter.
	roles, err := s.Roles(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range roles {
		if r.Name == "Builder 7 role" {
			if err = s.BindRole(ctx, owner, Binding{r.ID, "principal", worker.ID}, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	connection, err := s.SaveConnection(ctx, owner, Connection{Name: "Private route", Kind: "webhook", Enabled: true, Config: map[string]string{"url": "https://example.com/private"}, Secret: "test-only-private-route-secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveDestination(ctx, owner, Destination{PrincipalID: person.ID, ConnectionID: connection.ID, Address: "private-destination-address", Mode: "copy"})
	if err != nil {
		t.Fatal(err)
	}
	role := grant(t, s, owner, worker, []Rule{{Action: "view", TargetKind: "person", TargetID: "*"}, {Action: "message", TargetKind: "person", TargetID: person.ID, ConnectionID: connection.ID}, {Action: "message", TargetKind: "person", TargetID: hidden.ID}})
	grant(t, s, owner, viewer, []Rule{{Action: "view", TargetKind: "agent", TargetID: worker.ID}, {Action: "view", TargetKind: "access", TargetID: worker.ID}, {Action: "view", TargetKind: "person", TargetID: person.ID}})
	input := AgentContactsInput{Targets: []Target{{"person", person.ID}, {"person", hidden.ID}, {"person", person.ID}, {"agent", person.ID}, {"person", "per_missing"}}}
	result, err := s.AgentContacts(ctx, owner, worker.ID, input)
	if err != nil || len(result.Contacts) != 2 || len(result.Contacts[0].Routes) != 1 || result.Contacts[0].Routes[0].Kind != "webhook" {
		t.Fatal(result, err)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "private-destination-address") || strings.Contains(string(raw), connection.ID) || strings.Contains(string(raw), "test-only-private-route-secret") {
		t.Fatal("private routing data exposed")
	}
	result, err = s.AgentContacts(ctx, viewer, worker.ID, input)
	if err != nil || len(result.Contacts) != 1 || result.Contacts[0].Target.ID != person.ID || len(result.Contacts[0].Routes) != 0 {
		t.Fatal("operator privacy", result, err)
	}
	if _, err = s.AgentContacts(ctx, person, worker.ID, input); ErrorStatus(err) != 404 {
		t.Fatal("unprivileged policy inspection", err)
	}
	// A fresh observation does not retain the previous role grant.
	role.Rules = []Rule{{Action: "view", TargetKind: "person", TargetID: "*"}}
	if _, err = s.SaveRole(ctx, owner, role); err != nil {
		t.Fatal(err)
	}
	result, err = s.AgentContacts(ctx, owner, worker.ID, input)
	if err != nil || len(result.Contacts) != 0 {
		t.Fatal("stale contact policy", result, err)
	}
	if _, err = s.AgentContacts(ctx, owner, person.ID, input); ErrorStatus(err) != 404 {
		t.Fatal("non-agent subject", err)
	}
	if _, err = s.AgentContacts(ctx, owner, worker.ID, AgentContactsInput{Targets: []Target{{"group", "grp_example"}}}); ErrorStatus(err) != 400 {
		t.Fatal("group expansion", err)
	}
}
