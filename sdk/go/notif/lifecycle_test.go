package notif

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestRealStagedFilesAndSharedMembership(t *testing.T) {
	base := os.Getenv("NOTIF_SDK_TEST_URL")
	if base == "" || os.Getenv("NOTIF_SDK_EXPECT_CHAT_LIFECYCLE") == "" {
		t.Skip("Requires integrated file and membership engine")
	}
	ctx := context.Background()
	owner := New(base, os.Getenv("NOTIF_SDK_TEST_TOKEN"))
	var me struct {
		Person Person `json:"person"`
	}
	if err := owner.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); err != nil {
		t.Fatal(err)
	}
	sessions := []Session{}
	for range 3 {
		_, session, err := owner.Enroll(ctx, EnrollInput{Agent: "sdk/go-lifecycle/" + Key(), Name: "Go lifecycle worker"})
		if err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, session)
	}
	for _, session := range sessions {
		rules := []map[string]string{{"action": "view", "target_kind": "person", "target_id": me.Person.ID}}
		for _, peer := range sessions {
			if peer.Agent.ID != session.Agent.ID {
				rules = append(rules, map[string]string{"action": "view", "target_kind": "agent", "target_id": peer.Agent.ID})
			}
		}
		var role map[string]any
		if err := owner.Do(ctx, "POST", "/api/v1/roles", map[string]any{"name": "Lifecycle view " + Key(), "rules": rules}, &role, ""); err != nil {
			t.Fatal(err)
		}
		if err := owner.Do(ctx, "POST", "/api/v1/role-bindings", map[string]any{"role_id": role["id"], "subject_kind": "principal", "subject_id": session.Agent.ID}, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	member := New(base, sessions[2].Token)
	file, err := owner.StageAttachment(ctx, "before.txt", "text/plain", []byte("Before joining"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = member.DownloadAttachment(ctx, file.ID); err == nil {
		t.Fatal("private draft disclosed")
	}
	chat, err := owner.StartChat(ctx, StartChatInput{Targets: []Target{{Kind: "agent", ID: sessions[0].Agent.ID}, {Kind: "agent", ID: sessions[1].Agent.ID}}, Body: "Before joining", AttachmentIDs: []string{file.ID}}, Key())
	if err != nil || len(chat.Messages) != 1 || len(chat.Messages[0].Attachments) != 1 {
		t.Fatal(chat, err)
	}
	change, err := owner.ChangeChatMembership(ctx, chat.ConversationID, ChatMembershipInput{Operation: "add", Version: 1, Members: []string{sessions[2].Agent.ID}}, Key())
	if err != nil {
		t.Fatal(err)
	}
	var history struct {
		Messages []Message `json:"messages"`
	}
	raw, err := member.Conversation(ctx, chat.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &history)
	if len(history.Messages) != 0 {
		t.Fatal("earlier history shared by default", history)
	}
	if _, err = member.DownloadAttachment(ctx, file.ID); err == nil {
		t.Fatal("earlier file disclosed")
	}
	after, err := owner.StageAttachment(ctx, "after.txt", "text/plain", []byte("After joining"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Send(ctx, SendInput{ConversationID: chat.ConversationID, Body: "After joining", AttachmentIDs: []string{after.ID}}, Key()); err != nil {
		t.Fatal(err)
	}
	if data, err := member.DownloadAttachment(ctx, after.ID); err != nil || string(data) != "After joining" {
		t.Fatal(string(data), err)
	}
	change, err = owner.ChangeChatMembership(ctx, chat.ConversationID, ChatMembershipInput{Operation: "remove", Version: change.Version, Members: []string{sessions[2].Agent.ID}}, Key())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = member.DownloadAttachment(ctx, after.ID); err == nil {
		t.Fatal("removed member retained file access")
	}
	change, err = owner.ChangeChatMembership(ctx, chat.ConversationID, ChatMembershipInput{Operation: "add", Version: change.Version, Members: []string{sessions[2].Agent.ID}, ShareHistory: true}, Key())
	if err != nil {
		t.Fatal(err)
	}
	if data, err := member.DownloadAttachment(ctx, file.ID); err != nil || string(data) != "Before joining" {
		t.Fatal("explicit history share", string(data), err)
	}
	requestFile, err := owner.StageAttachment(ctx, "request.txt", "text/plain", []byte("Request attachment"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := owner.Ask(ctx, AskInput{Target: Target{Kind: "agent", ID: sessions[0].Agent.ID}, Body: "Review attachment", AttachmentIDs: []string{requestFile.ID}}, Key())
	if err != nil || len(request.Attachments) != 1 || request.Attachments[0].RequestID != request.ID {
		t.Fatal(request, err)
	}
	batchFile, err := owner.StageAttachment(ctx, "batch.txt", "text/plain", []byte("Private batch copies"))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := owner.SendBatch(ctx, SendBatchInput{Targets: []Target{{Kind: "agent", ID: sessions[0].Agent.ID}, {Kind: "agent", ID: sessions[1].Agent.ID}}, Body: "Private copies", AttachmentIDs: []string{batchFile.ID}}, Key())
	if err != nil || len(batch) != 2 {
		t.Fatal(batch, err)
	}
	for _, item := range batch {
		if len(item.Result.Messages) != 1 || len(item.Result.Messages[0].Attachments) != 1 {
			t.Fatal("missing batch association", item)
		}
	}
}

func TestRealRequestReplacementIsAtomicAndReplaysCurrentState(t *testing.T) {
	base := os.Getenv("NOTIF_SDK_TEST_URL")
	if base == "" {
		t.Skip("Requires real HTTP engine")
	}
	c := New(base, os.Getenv("NOTIF_SDK_TEST_TOKEN"))
	ctx := context.Background()
	_, session, err := c.Enroll(ctx, EnrollInput{Agent: "sdk/go-replace/" + Key(), Name: "Replacement worker"})
	if err != nil {
		t.Fatal(err)
	}
	original, err := c.Ask(ctx, AskInput{Target: Target{Kind: "agent", ID: session.Agent.ID}, Body: "Check staging now?", Kind: "approval"}, Key())
	if err != nil {
		t.Fatal(err)
	}
	input := ReplaceRequestInput{Version: original.Version, Body: "Check staging after the demo?"}
	key := Key()
	result, err := c.ReplaceRequest(ctx, original.ID, input, key)
	if err != nil || result.Previous.State != "superseded" || result.Replacement.ID == original.ID || result.Replacement.State != "open" {
		t.Fatal(result, err)
	}
	again, err := c.ReplaceRequest(ctx, original.ID, input, key)
	if err != nil || again.Replacement.ID != result.Replacement.ID {
		t.Fatal(again, err)
	}
}
