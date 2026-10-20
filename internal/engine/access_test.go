package engine

import (
	"context"
	"fmt"
	"testing"
)

func TestRoleRevocationRemovesHistoryAndReplay(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "source")
	person := testPerson(t, s, a, "reviewer")
	role := grant(t, s, a, person, []Rule{{Action: "view", TargetKind: "agent", TargetID: w.ID, ConnectionID: "", ActionName: ""}, {Action: "message", TargetKind: "agent", TargetID: w.ID, ConnectionID: "", ActionName: ""}})
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

func TestReplayCursorAdvancesAcrossHiddenPage(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "cursor-worker")
	p := testPerson(t, s, a, "cursor-person")
	role := grant(t, s, a, p, []Rule{{Action: "view", TargetKind: "agent", TargetID: w.ID, ConnectionID: "", ActionName: ""}, {Action: "message", TargetKind: "agent", TargetID: w.ID, ConnectionID: "", ActionName: ""}})
	for i := 0; i < 205; i++ {
		if _, e := s.Send(ctx, w, SendInput{Target: Target{"person", p.ID}, Body: "private"}, fmt.Sprint("cursor-", i)); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.BindRole(ctx, a, Binding{role.ID, "principal", p.ID}, true); e != nil {
		t.Fatal(e)
	}
	events, cursor, e := s.EventPage(ctx, p, 0, false)
	if e != nil || cursor < 200 {
		t.Fatal(len(events), cursor, e)
	}
	for _, ev := range events {
		if ev.ConversationID != "" {
			t.Fatal("revoked conversation leaked", ev.Kind)
		}
	}
	recent, _, e := s.EventPage(ctx, a, 0, true)
	if e != nil || len(recent) != 200 || recent[0].Sequence <= recent[1].Sequence {
		t.Fatal("recent activity order", e)
	}
}
