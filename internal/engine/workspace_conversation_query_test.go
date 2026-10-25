package engine

import (
	"context"
	"testing"
)

func TestConversationRequestQueryKeepsPrivateVisibility(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	reader := testPerson(t, s, owner, "conversation-request-reader")
	worker := testWorker(t, s, owner, "conversation-request-worker")
	one, err := s.Ask(ctx, owner, AskInput{Target: Target{"agent", worker.ID}, Kind: "question", Title: "One", Body: "Private one"}, "one")
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.Ask(ctx, owner, AskInput{Target: Target{"agent", worker.ID}, Kind: "task", Title: "Two", Body: "Private two"}, "two")
	if err != nil {
		t.Fatal(err)
	}
	in := QueryInput{Collection: "requests", Filters: map[string]string{"conversation_id": one.ConversationID}, Limit: 1}
	page, err := s.Query(ctx, owner, in)
	if err != nil || len(page.Items) != 1 || page.Items[0].(Request).ID != one.ID {
		t.Fatal(page, err)
	}
	for _, item := range page.Items {
		if item.(Request).ID == two.ID {
			t.Fatal("another conversation leaked")
		}
	}
	page, err = s.Query(ctx, reader, in)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("guessed conversation broadened request visibility", page, err)
	}
	foreign, err := s.Bootstrap(ctx, "conversation-query-foreign/test", "Foreign", "test", Secret())
	if err != nil {
		t.Fatal(err)
	}
	page, err = s.Query(ctx, foreign, in)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("cross-scope request leaked", page, err)
	}
}

func TestGroupNameEditPreservesMembershipAndVisibility(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "partial-name-lead")
	hidden := testPerson(t, s, owner, "partial-name-hidden")
	g, err := s.SaveGroup(ctx, owner, Group{Name: "Team", Members: []string{lead.ID, hidden.ID}})
	if err != nil {
		t.Fatal(err)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "group", TargetID: g.ID}})
	changed, err := s.SaveGroup(ctx, lead, Group{ID: g.ID, Version: g.Version, Name: "Renamed team"})
	// A group management grant also covers its members. The returned roster must
	// agree with current query visibility, rather than inventing a partial roster.
	visible, visibleErr := s.queryRecord(ctx, lead, "groups", g.ID)
	if visibleErr != nil {
		t.Fatal(visibleErr)
	}
	if err != nil || changed.Name != "Renamed team" || changed.MembersComplete != visible.(Group).MembersComplete || len(changed.Members) != len(visible.(Group).Members) {
		t.Fatal(changed, err)
	}
	full, err := s.queryRecord(ctx, owner, "groups", g.ID)
	if err != nil || len(full.(Group).Members) != 2 {
		t.Fatal("name edit changed membership", full, err)
	}
	outsider := testPerson(t, s, owner, "name-edit-outsider")
	if _, err = s.SaveGroup(ctx, outsider, Group{ID: g.ID, Version: changed.Version, Name: "Unauthorized"}); ErrorStatus(err) != 403 {
		t.Fatal("name-only update bypassed authority", err)
	}
	if _, err = s.SaveGroup(ctx, lead, Group{ID: g.ID, Version: g.Version, Name: "Stale rename"}); ErrorStatus(err) != 409 {
		t.Fatal("stale rename accepted", err)
	}
}
