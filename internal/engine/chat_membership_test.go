package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestChatMembershipHistoryFilesRequestsAndRejoin(t *testing.T) {
	s, owner := testStore(t)
	s.AttachmentsRoot = t.TempDir()
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	two := testWorker(t, s, owner, "Builder 2")
	newcomer := testWorker(t, s, owner, "DevOps 3")
	file, err := s.UploadAttachment(ctx, owner, "", "before.txt", "text/plain", []byte("earlier private history"))
	if err != nil {
		t.Fatal(err)
	}
	sent, err := s.StartChat(ctx, owner, StartChatInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "earlier private history", AttachmentIDs: []string{file.ID}}, "create")
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.Ask(ctx, one, AskInput{ConversationID: sent.ConversationID, Kind: "approval", Body: "May I apply this migration?"}, "before-request")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Conversation(ctx, owner, sent.ConversationID)
	if err != nil || !c.Shared {
		t.Fatal(c, err)
	}
	change := ChatMembershipInput{Operation: "add", Version: c.Version, Members: []string{newcomer.ID}}
	added, err := s.ChangeChatMembership(ctx, owner, c.ID, change, "add")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.ChangeChatMembership(ctx, owner, c.ID, change, "add")
	if err != nil || replay.Version != added.Version {
		t.Fatal(replay, err)
	}
	c, err = s.Conversation(ctx, newcomer, c.ID)
	if err != nil || len(c.Messages) != 0 || c.LastPreview != "" || len(c.Participants) != 4 || c.HistoryStart == 0 {
		t.Fatal(c, err)
	}
	if _, _, err = s.DownloadAttachment(ctx, newcomer, file.ID); ErrorStatus(err) != 404 {
		t.Fatal("old file exposed", err)
	}
	if _, err = s.Request(ctx, newcomer, req.ID); ErrorStatus(err) != 404 {
		t.Fatal("old approval exposed", err)
	}
	assertEmpty := func(collection, term string) {
		t.Helper()
		page, e := s.Query(ctx, newcomer, QueryInput{Collection: collection, Search: term, Filters: map[string]string{"conversation_id": c.ID}})
		if e != nil || len(page.Items) != 0 || page.NextCursor != "" {
			t.Fatal(collection, page, e)
		}
	}
	assertEmpty("messages", "earlier")
	assertEmpty("attachments", "before")
	assertEmpty("requests", "")
	listing, err := s.Conversations(ctx, newcomer)
	if err != nil || len(listing) != 1 || listing[0].LastPreview != "" {
		t.Fatal(listing, err)
	}
	page, err := s.Query(ctx, newcomer, QueryInput{Collection: "conversations", Search: "migration"})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("preview search leaked", page, err)
	}
	// Resolving a pre-join request cannot disclose it through a post-join event.
	if _, err = s.Respond(ctx, owner, req.ID, Response{Version: req.Version, Outcome: "approved"}); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(ctx, newcomer, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), req.ID) || strings.Contains(string(raw), "earlier private history") {
			t.Fatal("old event exposed", string(raw))
		}
	}
	follow, err := s.Send(ctx, owner, SendInput{ConversationID: c.ID, Body: "New messages are visible."}, "after")
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.Conversation(ctx, newcomer, c.ID)
	if err != nil || len(c.Messages) != 1 || c.Messages[0].ID != follow.Messages[0].ID {
		t.Fatal(c, err)
	}
	removed, err := s.ChangeChatMembership(ctx, owner, c.ID, ChatMembershipInput{Operation: "remove", Version: added.Version, Members: []string{newcomer.ID}}, "remove")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Conversation(ctx, newcomer, c.ID); ErrorStatus(err) != 404 {
		t.Fatal("removed member reads", err)
	}
	if _, err = s.Send(ctx, newcomer, SendInput{ConversationID: c.ID, Body: "cannot send"}, "removed-send"); ErrorStatus(err) != 404 {
		t.Fatal("removed member sends", err)
	}
	if inbox, err := s.Inbox(ctx, newcomer); err != nil || len(inbox) != 0 {
		t.Fatal("removed member claims queued history", inbox, err)
	}
	// Rejoining starts a fresh window; queued messages from the earlier membership stay hidden.
	rejoined, err := s.ChangeChatMembership(ctx, owner, c.ID, ChatMembershipInput{Operation: "add", Version: removed.Version, Members: []string{newcomer.ID}}, "rejoin")
	if err != nil {
		t.Fatal(err)
	}
	if inbox, err := s.Inbox(ctx, newcomer); err != nil || len(inbox) != 0 {
		t.Fatal("rejoin resurrected old delivery", inbox, err)
	}
	c, err = s.Conversation(ctx, newcomer, c.ID)
	if err != nil || len(c.Messages) != 0 {
		t.Fatal(c, err)
	}
	left, err := s.ChangeChatMembership(ctx, newcomer, c.ID, ChatMembershipInput{Operation: "leave", Version: rejoined.Version}, "leave")
	if err != nil || !left.Left {
		t.Fatal(left, err)
	}
	// Earlier history is an explicit disclosure choice, not an automatic side effect of adding someone.
	_, err = s.ChangeChatMembership(ctx, owner, c.ID, ChatMembershipInput{Operation: "add", Version: left.Version, Members: []string{newcomer.ID}, ShareHistory: true}, "share-history")
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.Conversation(ctx, newcomer, c.ID)
	if err != nil || len(c.Messages) != 2 {
		t.Fatal(c, err)
	}
	if _, f, e := s.DownloadAttachment(ctx, newcomer, file.ID); e != nil {
		t.Fatal(e)
	} else {
		f.Close()
	}
	if _, err = s.Request(ctx, newcomer, req.ID); err != nil {
		t.Fatal(err)
	}
}

func TestChatMembershipDisclosureManagementAndDecisionAuthority(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	two := testWorker(t, s, owner, "Builder 2")
	newcomer := testWorker(t, s, owner, "DevOps 3")
	stranger := testPerson(t, s, owner, "No grants")
	sent, err := s.StartChat(ctx, owner, StartChatInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "Ready"}, "chat")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Conversation(ctx, owner, sent.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChangeChatMembership(ctx, one, c.ID, ChatMembershipInput{Operation: "rename", Version: c.Version, Title: "No"}, "not-manager"); ErrorStatus(err) != 403 {
		t.Fatal(err)
	}
	if _, err = s.ChangeChatMembership(ctx, owner, c.ID, ChatMembershipInput{Operation: "add", Version: c.Version, Members: []string{stranger.ID}}, "no-disclosure"); ErrorStatus(err) != 403 {
		t.Fatal(err)
	}
	req, err := s.Ask(ctx, one, AskInput{ConversationID: c.ID, Kind: "approval", Body: "Approve migration?"}, "approval")
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.ChangeChatMembership(ctx, owner, c.ID, ChatMembershipInput{Operation: "add", Version: c.Version, Members: []string{newcomer.ID}, ShareHistory: true}, "disclose")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Request(ctx, newcomer, req.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, newcomer, req.ID, Response{Version: req.Version, Outcome: "approved"}); ErrorStatus(err) != 403 {
		t.Fatal("membership grants old decision audience", err)
	}
	if _, err = s.ChangeChatMembership(ctx, owner, c.ID, ChatMembershipInput{Operation: "rename", Version: c.Version, Title: "Stale"}, "stale"); ErrorStatus(err) != 409 {
		t.Fatal(err)
	}
	if _, err = s.ChangeChatMembership(ctx, owner, c.ID, ChatMembershipInput{Operation: "leave", Version: added.Version}, "no-successor"); ErrorStatus(err) != 400 {
		t.Fatal(err)
	}
	left, err := s.ChangeChatMembership(ctx, owner, c.ID, ChatMembershipInput{Operation: "leave", Version: added.Version, NextManager: one.ID}, "handoff")
	if err != nil || !left.Left {
		t.Fatal(left, err)
	}
	c, err = s.Conversation(ctx, one, c.ID)
	if err != nil || c.ManagerID != one.ID || len(c.Participants) != 3 || len(c.PastParticipants) != 1 {
		t.Fatal(c, err)
	}
	if _, err = s.ChangeChatMembership(ctx, one, c.ID, ChatMembershipInput{Operation: "rename", Version: left.Version, Title: "Migration team"}, "rename"); err != nil {
		t.Fatal(err)
	}
	// A shared two-member room is never mistaken for a private DM.
	pair, err := s.StartChat(ctx, owner, StartChatInput{Targets: []Target{{"agent", one.ID}}, Body: "Shared pair"}, "pair")
	if err != nil {
		t.Fatal(err)
	}
	direct, err := s.Send(ctx, owner, SendInput{Target: Target{"agent", one.ID}, ReuseDirect: true, Body: "Private"}, "dm")
	if err != nil || direct.ConversationID == pair.ConversationID {
		t.Fatal(direct, err)
	}
}

func TestChatMembershipRemovalSerializesWithSending(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	two := testWorker(t, s, owner, "Builder 2")
	sent, err := s.StartChat(ctx, owner, StartChatInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "Start"}, "start")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	var sendErr, removeErr error
	go func() {
		defer wg.Done()
		_, sendErr = s.Send(ctx, two, SendInput{ConversationID: sent.ConversationID, Body: "Racing message"}, "race")
	}()
	go func() {
		defer wg.Done()
		_, removeErr = s.ChangeChatMembership(ctx, owner, sent.ConversationID, ChatMembershipInput{Operation: "remove", Version: 1, Members: []string{two.ID}}, "remove")
	}()
	wg.Wait()
	if removeErr != nil || (sendErr != nil && ErrorStatus(sendErr) != 404) {
		t.Fatal(sendErr, removeErr)
	}
	if _, err = s.Conversation(ctx, two, sent.ConversationID); ErrorStatus(err) != 404 {
		t.Fatal(err)
	}
	if inbox, err := s.Inbox(ctx, two); err != nil || len(inbox) != 0 {
		t.Fatal(inbox, err)
	}
}
