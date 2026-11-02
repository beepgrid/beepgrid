package engine

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

func TestReplaceRequestPreservesHistoryFilesAndFreshDecision(t *testing.T) {
	s, owner := testStore(t)
	s.AttachmentsRoot = t.TempDir()
	ctx := context.Background()
	worker := testWorker(t, s, owner, "Deployment 2")
	file, err := s.UploadAttachment(ctx, worker, "", "plan.txt", "text/plain", []byte("bounded rollout plan"))
	if err != nil {
		t.Fatal(err)
	}
	action := &Action{Name: "restart", ExecutorID: worker.ID, Resource: "staging", ResourceVersion: "v2", OperationKey: "restart-staging"}
	original, err := s.Ask(ctx, worker, AskInput{Target: Target{"person", owner.ID}, Kind: "approval", Body: "Restart staging now?", AttachmentIDs: []string{file.ID}, Action: action}, "original")
	if err != nil {
		t.Fatal(err)
	}
	input := ReplaceRequestInput{Version: original.Version, Body: "Restart staging after the demo?"}
	replacement, err := s.ReplaceRequest(ctx, worker, original.ID, input, "replace")
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Previous.State != "superseded" || replacement.Replacement.State != "open" || replacement.Replacement.ID == original.ID || replacement.Replacement.ConversationID != original.ConversationID || len(replacement.Replacement.Attachments) != 1 || replacement.Replacement.Attachments[0].RequestID != replacement.Replacement.ID {
		t.Fatal(replacement)
	}
	if replacement.Replacement.Action == nil || replacement.Replacement.Action.Name != action.Name || replacement.Replacement.Action.OperationKey != action.OperationKey || replacement.Replacement.Action.ResourceVersion != action.ResourceVersion || replacement.Replacement.Action.ExecutorID != worker.ID {
		t.Fatal("bound action changed", replacement.Replacement.Action)
	}
	if _, err = s.Respond(ctx, owner, original.ID, Response{Version: original.Version, Outcome: "approved"}); ErrorStatus(err) != 409 {
		t.Fatal("obsolete approval accepted", err)
	}
	approved, err := s.Respond(ctx, owner, replacement.Replacement.ID, Response{Version: 1, Outcome: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Redeem(ctx, worker, original.ID, *action); ErrorStatus(err) != 409 {
		t.Fatal("superseded action redeemed", err)
	}
	if _, err = s.Redeem(ctx, worker, approved.ID, *action); err != nil {
		t.Fatal("fresh bound approval could not be redeemed", err)
	}
	replay, err := s.ReplaceRequest(ctx, worker, original.ID, input, "replace")
	if err != nil || replay.Replacement.ID != approved.ID || replay.Replacement.State != "resolved" {
		t.Fatal("replay did not return current request", replay, err)
	}
	if _, err = s.ReplaceRequest(ctx, worker, original.ID, ReplaceRequestInput{Version: 1, Body: "Different body"}, "replace"); ErrorStatus(err) != 409 {
		t.Fatal("changed key", err)
	}
	if _, err = s.ReplaceRequest(ctx, owner, replacement.Replacement.ID, input, "not-mine"); ErrorStatus(err) != 403 {
		t.Fatal("admin impersonation", err)
	}
	_, reader, err := s.DownloadAttachment(ctx, owner, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	bytes, err := io.ReadAll(reader)
	if err != nil || string(bytes) != "bounded rollout plan" {
		t.Fatal("file lost", err)
	}
	var count int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM requests WHERE scope=$1`, owner.Scope).Scan(&count)
	if count != 2 {
		t.Fatal(count)
	}
}

func TestReplaceRequestKeepsPrivateProjectionsAndExactInstance(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	two := testWorker(t, s, owner, "Builder 2")
	group, err := s.SaveGroup(ctx, owner, Group{Name: "Builders", Purpose: "agent_role", Members: []string{one.ID, two.ID}})
	if err != nil {
		t.Fatal(err)
	}
	request, err := s.Ask(ctx, owner, AskInput{Target: Target{"group", group.ID}, Body: "Report rollout blockers"}, "ask")
	if err != nil {
		t.Fatal(err)
	}
	before1, _ := s.Request(ctx, one, request.ID)
	before2, _ := s.Request(ctx, two, request.ID)
	next, err := s.ReplaceRequest(ctx, owner, request.ID, ReplaceRequestInput{Version: 1, Body: "Report checkout rollout blockers only"}, "replace")
	if err != nil {
		t.Fatal(err)
	}
	after1, err := s.Request(ctx, one, next.Replacement.ID)
	if err != nil {
		t.Fatal(err)
	}
	after2, err := s.Request(ctx, two, next.Replacement.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before1.ConversationID != after1.ConversationID || before2.ConversationID != after2.ConversationID || after1.ConversationID == after2.ConversationID {
		t.Fatal("private projections merged", next)
	}
	if _, err = s.Conversation(ctx, one, after2.ConversationID); ErrorStatus(err) != 404 {
		t.Fatal("private cross-read", err)
	}
	exact, err := s.Ask(ctx, owner, AskInput{Target: Target{"instance", one.InstanceID}, Body: "Check this connection"}, "exact")
	if err != nil {
		t.Fatal(err)
	}
	exactNext, err := s.ReplaceRequest(ctx, owner, exact.ID, ReplaceRequestInput{Version: 1, Body: "Check only this connection again"}, "exact-next")
	if err != nil {
		t.Fatal(err)
	}
	var instance string
	err = s.Pool.QueryRow(ctx, `SELECT exact_instance FROM deliveries WHERE scope=$1 AND operation_id=$2 AND connection_id=''`, owner.Scope, exactNext.Replacement.ID).Scan(&instance)
	if err != nil || instance != one.InstanceID {
		t.Fatal(instance, err)
	}
}

func TestReplaceRequestRechecksPermissionsAndSharedMembership(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	two := testWorker(t, s, owner, "Reviewer 2")
	person := testPerson(t, s, owner, "Maya")
	role := grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "agent", TargetID: one.ID}, {Action: "ask", TargetKind: "agent", TargetID: one.ID}})
	request, err := s.Ask(ctx, person, AskInput{Target: Target{"agent", one.ID}, Body: "Review this migration"}, "ask")
	if err != nil {
		t.Fatal(err)
	}
	role.Rules = []Rule{{Action: "view", TargetKind: "agent", TargetID: one.ID}}
	if _, err = s.SaveRole(ctx, owner, role); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReplaceRequest(ctx, person, request.ID, ReplaceRequestInput{Version: 1, Body: "Review the new plan"}, "denied"); ErrorStatus(err) != 403 {
		t.Fatal("revoked send", err)
	}
	current, _ := s.Request(ctx, person, request.ID)
	if current.State != "open" {
		t.Fatal("failed replacement retired original", current)
	}
	chat, err := s.StartChat(ctx, owner, StartChatInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "Release room"}, "room")
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.Ask(ctx, owner, AskInput{ConversationID: chat.ConversationID, Kind: "approval", Body: "Restart staging?"}, "room-request")
	if err != nil {
		t.Fatal(err)
	}
	room, _ := s.Conversation(ctx, owner, chat.ConversationID)
	if _, err = s.ChangeChatMembership(ctx, owner, room.ID, ChatMembershipInput{Operation: "remove", Version: room.Version, Members: []string{two.ID}}, "remove"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReplaceRequest(ctx, owner, q.ID, ReplaceRequestInput{Version: 1, Body: "Restart staging after the demo?"}, "left"); ErrorStatus(err) != 409 {
		t.Fatal("removed recipient silently retargeted", err)
	}
	current, _ = s.Request(ctx, owner, q.ID)
	if current.State != "open" {
		t.Fatal(current)
	}
}

func TestReplaceRequestAndDecisionRaceHasOneWinner(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "Deployment")
	for i := 0; i < 8; i++ {
		q, err := s.Ask(ctx, worker, AskInput{Target: Target{"person", owner.ID}, Kind: "approval", Body: "Restart staging?", ExpiresAt: time.Now().Add(time.Hour)}, ID("key"))
		if err != nil {
			t.Fatal(err)
		}
		var replacement ReplacedRequest
		var replaceErr, replyErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			replacement, replaceErr = s.ReplaceRequest(ctx, worker, q.ID, ReplaceRequestInput{Version: 1, Body: "Restart staging later?"}, ID("replace"))
		}()
		go func() {
			defer wg.Done()
			_, replyErr = s.Respond(ctx, owner, q.ID, Response{Version: 1, Outcome: "approved"})
		}()
		wg.Wait()
		if (replaceErr == nil) == (replyErr == nil) {
			t.Fatal("race did not select one winner", replaceErr, replyErr)
		}
		if replaceErr == nil && replacement.Replacement.Response != nil {
			t.Fatal("old approval carried over")
		}
	}
}
