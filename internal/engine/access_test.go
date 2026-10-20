package engine

import (
	"context"
	"testing"
)

func TestRoleRevocationRemovesHistoryAndReplay(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "source")
	person := testPerson(t, s, a, "reviewer")
	role := grant(t, s, a, person, []Rule{{"view", "agent", w.ID, "", ""}, {"message", "agent", w.ID, "", ""}})
	sent, e := s.Send(ctx, w, SendInput{Target: Target{"person", person.ID}, Body: "Sensitive report"}, "private-report")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Conversation(ctx, person, sent.ConversationID); e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, a, Binding{role.ID, "principal", person.ID}, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Conversation(ctx, person, sent.ConversationID); ErrorStatus(e) != 404 {
		t.Fatal("history remained visible after role revocation", e)
	}
	events, e := s.Events(ctx, person, 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, ev := range events {
		if ev.ConversationID == sent.ConversationID {
			t.Fatal("revoked history leaked through replay")
		}
	}
}
func TestOIDCUsesVerifiedSubjectNotMatchingEmail(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	manual, e := s.CreatePerson(ctx, a, CreatePerson{ExternalID: "employee", Name: "Manual person", Email: "same@example.test"})
	if e != nil {
		t.Fatal(e)
	}
	token, e := s.LoginOIDC(ctx, a.Scope, "https://issuer.test", "subject-1", "SSO person", "same@example.test")
	if e != nil {
		t.Fatal(e)
	}
	person, e := s.Authenticate(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	if person.ID == manual.ID || person.Kind != "person" || person.Admin {
		t.Fatal("SSO email merged identity or acquired admin")
	}
	token2, e := s.LoginOIDC(ctx, a.Scope, "https://issuer.test", "subject-1", "Renamed", "same@example.test")
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Authenticate(ctx, token2)
	if e != nil || again.ID != person.ID {
		t.Fatal("SSO subject was not stable", e)
	}
	if e = s.SetDisabled(ctx, a, person.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.LoginOIDC(ctx, a.Scope, "https://issuer.test", "subject-1", "SSO person", "same@example.test"); ErrorStatus(e) != 403 {
		t.Fatal("disabled SSO person logged in", e)
	}
}
