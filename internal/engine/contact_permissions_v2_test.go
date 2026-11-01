package engine

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRequestScopedRepliesAcrossDirectionalPermissions(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	a := testPerson(t, s, owner, "requester")
	b := testPerson(t, s, owner, "recipient")
	ar := grant(t, s, owner, a, nil)
	br := grant(t, s, owner, b, nil)
	actions := []string{"view", "message", "ask", "decide"}
	set := func(role *Role, peer Actor, mask int) {
		role.Rules = nil
		for bit, action := range actions {
			if mask&(1<<bit) != 0 {
				role.Rules = append(role.Rules, Rule{Action: action, TargetKind: peer.Kind, TargetID: peer.ID})
			}
		}
		var err error
		*role, err = s.SaveRole(ctx, owner, *role)
		if err != nil {
			t.Fatal(err)
		}
	}
	for forward := 0; forward < 16; forward++ {
		set(&ar, b, forward)
		for reverse := 0; reverse < 16; reverse++ {
			set(&br, a, reverse)
			for _, kind := range []string{"question", "task", "approval"} {
				key := fmt.Sprintf("matrix/%d/%d/%s", forward, reverse, kind)
				r, err := s.Ask(ctx, a, AskInput{Target: Target{b.Kind, b.ID}, Kind: kind, Body: "Report the outcome"}, key)
				want := forward&4 != 0 && reverse&1 != 0 && (kind != "approval" || reverse&8 != 0)
				if !want {
					if ErrorStatus(err) != 403 {
						t.Fatalf("%s accepted unusable request: %v", key, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("%s: %v", key, err)
				}
				cap, err := s.Capabilities(ctx, b, CapabilityInput{RequestID: r.ID})
				if err != nil || !cap.Operations["respond"] {
					t.Fatalf("%s capabilities: %+v %v", key, cap, err)
				}
				outcome := map[string]string{"question": "answered", "task": "completed", "approval": "approved"}[kind]
				if _, err = s.Respond(ctx, b, r.ID, Response{Outcome: outcome, Version: 1, Answer: "done"}); err != nil {
					t.Fatalf("%s reply: %v", key, err)
				}
				if reverse&2 == 0 {
					if _, err = s.Send(ctx, b, SendInput{Target: Target{a.Kind, a.ID}, Body: "unrelated message"}, key+"/extra"); ErrorStatus(err) != 403 {
						t.Fatalf("%s reply granted general messaging: %v", key, err)
					}
				}
			}
		}
	}
}

func TestContactBlocksOverrideBothGroupDirectionsAndPendingDelivery(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	a := testWorker(t, s, owner, "source")
	b := testWorker(t, s, owner, "blocked")
	c := testWorker(t, s, owner, "allowed")
	g, err := s.SaveGroup(ctx, owner, Group{Name: "Reviewers", Purpose: "agent_role", Members: []string{b.ID, c.ID}})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := s.StartChat(ctx, a, StartChatInput{Targets: []Target{{b.Kind, b.ID}, {c.Kind, c.ID}}, Body: "Review this together"}, "room")
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.Send(ctx, a, SendInput{Target: Target{"group", g.ID}, Body: "Before the exception"}, "queued")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := s.SaveContactPermissions(ctx, owner, ContactPermissionsInput{Subject: Target{a.Kind, a.ID}, Rules: []Rule{{Action: "message", TargetKind: b.Kind, TargetID: b.ID, Effect: "deny"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, a, SendInput{Target: Target{b.Kind, b.ID}, Body: "direct"}, "blocked-direct"); ErrorStatus(err) != 403 {
		t.Fatal("direct bypass", err)
	}
	if _, err = s.Send(ctx, a, SendInput{ConversationID: chat.ConversationID, Body: "shared"}, "blocked-shared"); ErrorStatus(err) != 403 {
		t.Fatal("shared bypass", err)
	}
	sent, err := s.Send(ctx, a, SendInput{Target: Target{"group", g.ID}, Body: "Only permitted members"}, "filtered")
	if err != nil || len(sent.Messages) != 1 {
		t.Fatal("group exception bypass", sent, err)
	}
	inbox, err := s.Inbox(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range inbox {
		for _, m := range queued.Messages {
			if d.OperationID == m.ID {
				t.Fatal("queued group delivery bypassed current individual block")
			}
		}
	}
	// Incoming group targeting and outgoing inherited grants use the same block.
	team, err := s.SaveGroup(ctx, owner, Group{Name: "Builders", Purpose: "agent_role", Members: []string{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	inherited, err := s.SaveRole(ctx, owner, Role{Name: "Builder contacts", Rules: []Rule{{Action: "message", TargetKind: "group", TargetID: g.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindRole(ctx, owner, Binding{inherited.ID, "group", team.ID}, false); err != nil {
		t.Fatal(err)
	}
	for _, members := range [][]string{{}, {a.ID}} {
		team.Members = members
		team, err = s.SaveGroup(ctx, owner, team)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Send(ctx, a, SendInput{Target: Target{b.Kind, b.ID}, Body: "membership changed"}, ID("case")); ErrorStatus(err) != 403 {
			t.Fatal("membership cleared exception", err)
		}
	}
	policy.Rules = nil
	if _, err = s.SaveContactPermissions(ctx, owner, ContactPermissionsInput{Subject: Target{a.Kind, a.ID}, Version: policy.Version, Rules: policy.Rules}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, a, SendInput{Target: Target{b.Kind, b.ID}, Body: "Restored defaults"}, "defaults"); err != nil {
		t.Fatal(err)
	}
}

func TestScopedReplyRechecksVisibilityAskBlockAudienceAndRequestState(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	a := testPerson(t, s, owner, "asking")
	b := testPerson(t, s, owner, "answering")
	other := testPerson(t, s, owner, "outsider")
	askRole := grant(t, s, owner, a, []Rule{{Action: "ask", TargetKind: b.Kind, TargetID: b.ID}})
	viewRole := grant(t, s, owner, b, []Rule{{Action: "view", TargetKind: a.Kind, TargetID: a.ID}})
	input := AskInput{Target: Target{b.Kind, b.ID}, Body: "What is the result?"}
	r, err := s.Ask(ctx, a, input, "initial")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, other, r.ID, Response{Outcome: "answered", Answer: "intruder", Version: 1}); ErrorStatus(err) != 404 {
		t.Fatal("non-audience responded", err)
	}
	deny, err := s.SaveContactPermissions(ctx, owner, ContactPermissionsInput{Subject: Target{b.Kind, b.ID}, Rules: []Rule{{Action: "message", TargetKind: a.Kind, TargetID: a.ID, Effect: "deny"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, b, r.ID, Response{Outcome: "answered", Answer: "blocked", Version: 1}); ErrorStatus(err) != 403 {
		t.Fatal("block bypass", err)
	}
	if _, err = s.Ask(ctx, a, input, "no-responder"); ErrorStatus(err) != 403 {
		t.Fatal("unusable request accepted", err)
	}
	if _, err = s.SaveContactPermissions(ctx, owner, ContactPermissionsInput{Subject: Target{b.Kind, b.ID}, Version: deny.Version, Rules: []Rule{}}); err != nil {
		t.Fatal(err)
	}
	if err = s.BindRole(ctx, owner, Binding{viewRole.ID, "principal", b.ID}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, b, r.ID, Response{Outcome: "answered", Answer: "hidden", Version: 1}); ErrorStatus(err) != 404 {
		t.Fatal("visibility revoked", err)
	}
	if err = s.BindRole(ctx, owner, Binding{viewRole.ID, "principal", b.ID}, false); err != nil {
		t.Fatal(err)
	}
	if err = s.BindRole(ctx, owner, Binding{askRole.ID, "principal", a.ID}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, b, r.ID, Response{Outcome: "answered", Answer: "ask revoked", Version: 1}); ErrorStatus(err) != 403 {
		t.Fatal("ask revocation bypass", err)
	}
	if err = s.BindRole(ctx, owner, Binding{askRole.ID, "principal", a.ID}, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE requests SET expires_at=now()-interval '1 second' WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, b, r.ID, Response{Outcome: "answered", Answer: "late", Version: 1}); ErrorStatus(err) != 409 {
		t.Fatal("expired request answered", err)
	}
	input.ExpiresAt = time.Now().Add(time.Hour)
	fresh, err := s.Ask(ctx, a, input, "fresh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, b, fresh.ID, Response{Outcome: "answered", Answer: "bounded result", Version: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, b, SendInput{ConversationID: fresh.ConversationID, Body: "another message"}, "not-granted"); ErrorStatus(err) != 403 {
		t.Fatal("reply expanded conversation authority", err)
	}
}

func TestContactPreviewSourcesDefaultsBlocksAndVersion(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	a := testPerson(t, s, owner, "member")
	b := testWorker(t, s, owner, "contact")
	group, err := s.SaveGroup(ctx, owner, Group{Name: "Engineering", Members: []string{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	role, err := s.SaveRole(ctx, owner, Role{Name: "Team contacts", Rules: []Rule{{Action: "view", TargetKind: b.Kind, TargetID: b.ID}, {Action: "message", TargetKind: b.Kind, TargetID: b.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindRole(ctx, owner, Binding{role.ID, "group", group.ID}, false); err != nil {
		t.Fatal(err)
	}
	in := ContactPreviewInput{Subject: Target{a.Kind, a.ID}, Targets: []Target{{b.Kind, b.ID}}}
	current, err := s.PreviewContactPermissions(ctx, owner, in)
	if err != nil || !current.Contacts[0].Permissions["message"].Allowed || current.Contacts[0].Permissions["message"].Sources[0] != "Via Engineering" {
		t.Fatal(current, err)
	}
	rules := []Rule{{Action: "message", TargetKind: b.Kind, TargetID: b.ID, Effect: "deny"}}
	in.Rules = &rules
	preview, err := s.PreviewContactPermissions(ctx, owner, in)
	if err != nil || preview.Contacts[0].Permissions["message"].Allowed || !preview.Contacts[0].Permissions["message"].Blocked {
		t.Fatal(preview, err)
	}
	if !s.allowed(ctx, s.Pool, a, "message", Target{b.Kind, b.ID}, "", "") {
		t.Fatal("preview changed policy")
	}
	saved, err := s.SaveContactPermissions(ctx, owner, ContactPermissionsInput{Subject: in.Subject, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PreviewContactPermissions(ctx, owner, in); ErrorStatus(err) != 409 {
		t.Fatal("stale preview", err)
	}
	in.Version = saved.Version
	rules = []Rule{}
	restored, err := s.PreviewContactPermissions(ctx, owner, in)
	if err != nil || !restored.Contacts[0].Permissions["message"].Allowed {
		t.Fatal(restored, err)
	}
	outsider := testPerson(t, s, owner, "outside-access")
	if _, err = s.PreviewContactPermissions(ctx, outsider, in); ErrorStatus(err) != 404 {
		t.Fatal("unauthorized policy inspection", err)
	}
}

func TestUnassignedContactPolicyCanBeEditedWithoutRestoringOldGrants(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	a := testPerson(t, s, owner, "unassigned")
	b := testWorker(t, s, owner, "other")
	policy, err := s.SaveContactPermissions(ctx, owner, ContactPermissionsInput{Subject: Target{a.Kind, a.ID}, Rules: []Rule{{Action: "message", TargetKind: b.Kind, TargetID: b.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindRole(ctx, owner, Binding{policy.ID, "principal", a.ID}, true); err != nil {
		t.Fatal(err)
	}
	input := ContactPreviewInput{Subject: Target{a.Kind, a.ID}, Targets: []Target{{b.Kind, b.ID}}}
	preview, err := s.PreviewContactPermissions(ctx, owner, input)
	if err != nil || preview.Policy.Version != policy.Version || preview.Contacts[0].Permissions["message"].Allowed {
		t.Fatal(preview, err)
	}
	rules := []Rule{{Action: "view", TargetKind: b.Kind, TargetID: b.ID}}
	input.Rules = &rules
	input.Version = preview.Policy.Version
	if _, err = s.PreviewContactPermissions(ctx, owner, input); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveContactPermissions(ctx, owner, ContactPermissionsInput{Subject: input.Subject, Version: input.Version, Rules: rules}); err != nil {
		t.Fatal(err)
	}
}

func TestBlocksCannotBecomeDelegationAuthority(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	lead := testPerson(t, s, owner, "delegated")
	peer := testWorker(t, s, owner, "private")
	grant(t, s, owner, lead, []Rule{{Action: "*", TargetKind: "*", TargetID: "*"}, {Action: "message", TargetKind: peer.Kind, TargetID: peer.ID, Effect: "deny"}})
	if _, err := s.SaveRole(ctx, lead, Role{Name: "Bypass", Rules: []Rule{{Action: "message", TargetKind: peer.Kind, TargetID: peer.ID}}}); ErrorStatus(err) != 403 {
		t.Fatal("blocked wildcard grant was delegated", err)
	}
	for _, rule := range []Rule{{Effect: "wrong", Action: "message", TargetKind: peer.Kind, TargetID: peer.ID}, {Effect: "deny", Action: "manage", TargetKind: peer.Kind, TargetID: peer.ID}, {Effect: "deny", Action: "message", TargetKind: peer.Kind, TargetID: peer.ID, ConnectionID: "provider"}} {
		if _, err := s.SaveRole(ctx, owner, Role{Name: "Invalid block", Rules: []Rule{rule}}); ErrorStatus(err) != 400 {
			t.Fatal("invalid effect accepted", rule, err)
		}
	}
}
