package engine

import (
	"context"
	"testing"
	"time"
)

func TestSharedChatDurabilityVisibilityAndReplay(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	two := testWorker(t, s, owner, "DevOps 2")
	in := StartChatInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "@DevOps 2 keep the migration below ten jobs."}
	first, err := s.StartChat(ctx, owner, in, "shared-chat")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.StartChat(ctx, owner, in, "shared-chat")
	if err != nil || replay.ConversationID != first.ConversationID || replay.Messages[0].ID != first.Messages[0].ID {
		t.Fatal(replay, err)
	}
	for _, actor := range []Actor{owner, one, two} {
		c, err := s.Conversation(ctx, actor, first.ConversationID)
		if err != nil || len(c.Participants) != 3 || len(c.Messages) != 1 {
			t.Fatal(c, err)
		}
	}
	if _, err = s.Send(ctx, one, SendInput{ConversationID: first.ConversationID, Body: "Migrating to the V2 endpoint now."}, "builder-reply"); err != nil {
		t.Fatal(err)
	}
	c, err := s.Conversation(ctx, two, first.ConversationID)
	if err != nil || len(c.Messages) != 2 {
		t.Fatal(c, err)
	}
	stranger := testPerson(t, s, owner, "Other person")
	if _, err = s.Conversation(ctx, stranger, first.ConversationID); ErrorStatus(err) != 404 {
		t.Fatal("shared chat leaked", err)
	}
	if _, err = s.StartChat(ctx, owner, StartChatInput{Targets: []Target{{"person", stranger.ID}, {"agent", one.ID}}, Body: "Do not disclose this"}, "bad-chat"); ErrorStatus(err) != 403 {
		t.Fatal(err)
	}
	var count int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE scope=$1 AND body='Do not disclose this'`, owner.Scope).Scan(&count)
	if count != 0 {
		t.Fatal("failed chat committed")
	}
}
func TestCommunicationApprovalHasNoExecutionAuthorityOrImplicitDeadline(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "Deployment 2")
	request, err := s.Ask(ctx, worker, AskInput{Target: Target{"person", owner.ID}, Kind: "approval", Body: "May I delete the temporary staging table?"}, "decision")
	if err != nil || request.Action != nil || request.Title == "" || request.ExpiresAt.Before(time.Now().AddDate(100, 0, 0)) {
		t.Fatal(request, err)
	}
	if _, err = s.Send(ctx, owner, SendInput{ConversationID: request.ConversationID, Body: "How long will staging be busy?"}, "followup"); err != nil {
		t.Fatal(err)
	}
	approved, err := s.Respond(ctx, owner, request.ID, Response{Version: request.Version, Outcome: "approved"})
	if err != nil || approved.State != "resolved" {
		t.Fatal(approved, err)
	}
	if _, err = s.Redeem(ctx, worker, request.ID, Action{Name: "delete"}); ErrorStatus(err) != 409 {
		t.Fatal("communication decision became execution authority", err)
	}
}

func TestSharedChatFirstMessageMentionsAreAtomicAndDoNotExcludeObservers(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 7")
	two := testWorker(t, s, owner, "DevOps 2")
	in := StartChatInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "@DevOps 2 keep concurrency at eight.", Mentions: []Mention{{PrincipalID: two.ID, Start: 0, End: 9}}}
	first, err := s.StartChat(ctx, owner, in, "first-mention")
	if err != nil || len(first.Messages) != 1 || len(first.Messages[0].Mentions) != 1 {
		t.Fatal(first, err)
	}
	for _, recipient := range []Actor{one, two} {
		inbox, err := s.Inbox(ctx, recipient)
		if err != nil || len(inbox) != 1 || inbox[0].Payload["type"] != "message" {
			t.Fatal("all room members must receive the original", inbox, err)
		}
	}
	again, err := s.StartChat(ctx, owner, in, "first-mention")
	if err != nil || again.ConversationID != first.ConversationID || again.Messages[0].ID != first.Messages[0].ID {
		t.Fatal(again, err)
	}
	for _, approval := range []bool{false, true} {
		decision := in
		decision.Approval = approval
		if !approval {
			decision.Choices = []string{"Now", "After the demo"}
		}
		result, err := s.StartChat(ctx, owner, decision, ID("decision"))
		if err != nil {
			t.Fatal(err)
		}
		page, err := s.Query(ctx, owner, QueryInput{Collection: "requests", Filters: map[string]string{"conversation_id": result.ConversationID}})
		if err != nil || len(page.Items) != 1 || len(page.Items[0].(Request).Mentions) != 1 {
			t.Fatal(page, err)
		}
	}
	stranger := testWorker(t, s, owner, "Billing 2")
	invalid := in
	invalid.Mentions = []Mention{{PrincipalID: stranger.ID, Start: 0, End: 9}}
	var before, after int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE scope=$1`, owner.Scope).Scan(&before)
	if _, err = s.StartChat(ctx, owner, invalid, "outside-member"); ErrorStatus(err) != 400 {
		t.Fatal("mention cannot invite someone", err)
	}
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE scope=$1`, owner.Scope).Scan(&after)
	if before != after {
		t.Fatal("invalid mention left an orphaned room", before, after)
	}
}
func TestSharedChatCanStartWithDecisionAndReplay(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 21")
	two := testWorker(t, s, owner, "DevOps 22")
	for _, approval := range []bool{false, true} {
		key := "choices"
		in := StartChatInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "Can we use staging for this migration?", Choices: []string{"Now", "After the demo"}}
		if approval {
			key = "approval"
			in.Approval, in.Choices = true, nil
		}
		first, err := s.StartChat(ctx, owner, in, key)
		if err != nil {
			t.Fatal(err)
		}
		again, err := s.StartChat(ctx, owner, in, key)
		if err != nil || first.ConversationID != again.ConversationID {
			t.Fatal(again, err)
		}
		thread, err := s.Conversation(ctx, one, first.ConversationID)
		if err != nil || len(thread.Participants) != 3 {
			t.Fatal(thread, err)
		}
		page, err := s.Query(ctx, one, QueryInput{Collection: "requests", Filters: map[string]string{"conversation_id": first.ConversationID}})
		if err != nil || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
		if _, err = s.Send(ctx, two, SendInput{ConversationID: first.ConversationID, Body: "After the demo works."}, key+"/followup"); err != nil {
			t.Fatal(err)
		}
	}
}
func TestContactPoliciesAreAtomicScopedAndVersioned(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	person := testPerson(t, s, owner, "Maya")
	agent := testWorker(t, s, owner, "Builder 9")
	in := ContactPermissionsInput{Subject: Target{"person", person.ID}, Rules: []Rule{{Action: "view", TargetKind: "agent", TargetID: agent.ID}, {Action: "message", TargetKind: "agent", TargetID: agent.ID}}}
	policy, err := s.SaveContactPermissions(ctx, owner, in)
	if err != nil || policy.Version != 1 {
		t.Fatal(policy, err)
	}
	if _, err = s.Send(ctx, person, SendInput{Target: Target{"agent", agent.ID}, Body: "Hello"}, "allowed"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveContactPermissions(ctx, owner, in); ErrorStatus(err) != 409 {
		t.Fatal("stale overwrite", err)
	}
	in.Version = policy.Version
	in.Rules = nil
	if _, err = s.SaveContactPermissions(ctx, owner, in); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, person, SendInput{Target: Target{"agent", agent.ID}, Body: "Blocked"}, "blocked"); ErrorStatus(err) != 403 {
		t.Fatal(err)
	}
	in.Version = 2
	in.Rules = []Rule{{Action: "manage", TargetKind: "agent", TargetID: agent.ID}}
	if _, err = s.SaveContactPermissions(ctx, owner, in); ErrorStatus(err) != 400 {
		t.Fatal("tool authority added", err)
	}
	in.Rules = []Rule{{Action: "message", TargetKind: "agent", TargetID: agent.ID}}
	if _, err = s.SaveContactPermissions(ctx, person, in); ErrorStatus(err) != 403 {
		t.Fatal("self escalation", err)
	}
}
func TestTeamAndAgentRoleMembershipStayDistinct(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	agent := testWorker(t, s, owner, "Builder 1")
	team, err := s.SaveGroup(ctx, owner, Group{Name: "Engineering", Purpose: "team", Members: []string{owner.ID}})
	if err != nil {
		t.Fatal(err)
	}
	team.Members = append(team.Members, agent.ID)
	if _, err = s.SaveGroup(ctx, owner, team); ErrorStatus(err) != 400 {
		t.Fatal("agent in people team", err)
	}
	role, err := s.SaveGroup(ctx, owner, Group{Name: "Builders", Purpose: "agent_role", Members: []string{agent.ID}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Query(ctx, owner, QueryInput{Collection: "groups", Filters: map[string]string{"purpose": "agent_role"}})
	if err != nil || len(page.Items) != 1 || page.Items[0].(Group).ID != role.ID || page.Items[0].(Group).Purpose != "agent_role" {
		t.Fatal(page, err)
	}
}

func TestAttentionFiltersExcludeObservableAgentTraffic(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 11")
	two := testWorker(t, s, owner, "Billing 12")
	mine, err := s.Ask(ctx, one, AskInput{Target: Target{"person", owner.ID}, Body: "Can I restart staging?", Kind: "approval"}, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Ask(ctx, one, AskInput{Target: Target{"agent", two.ID}, Body: "Check the payment ledger."}, "theirs"); err != nil {
		t.Fatal(err)
	}
	page, err := s.Query(ctx, owner, QueryInput{Collection: "requests", Filters: map[string]string{"recipient_id": owner.ID, "state": "open"}})
	if err != nil || len(page.Items) != 1 || page.Items[0].(Request).ID != mine.ID {
		t.Fatal(page, err)
	}
	personal, err := s.Query(ctx, owner, QueryInput{Collection: "conversations", Filters: map[string]string{"participant_id": owner.ID}})
	if err != nil || len(personal.Items) != 1 || personal.Items[0].(Conversation).ID != mine.ConversationID {
		t.Fatal(personal, err)
	}
}
func TestContactPoliciesForSeveralSubjectsDoNotCollide(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testPerson(t, s, owner, "One")
	two := testPerson(t, s, owner, "Two")
	var ids []string
	for _, p := range []Actor{one, two} {
		policy, err := s.SaveContactPermissions(ctx, owner, ContactPermissionsInput{Subject: Target{"person", p.ID}, Rules: []Rule{{Action: "view", TargetKind: "person", TargetID: owner.ID}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, policy.ID)
	}
	if ids[0] == ids[1] {
		t.Fatal("policy collision")
	}
}
