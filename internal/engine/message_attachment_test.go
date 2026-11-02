package engine

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestDraftFilesBecomeVisibleOnlyWithCommittedMessage(t *testing.T) {
	s, owner := testStore(t)
	s.AttachmentsRoot = t.TempDir()
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	two := testWorker(t, s, owner, "Billing 2")
	draft, err := s.UploadAttachment(ctx, owner, "", "orders.pdf", "application/pdf", []byte("%PDF-1.4\norders 701 702"))
	if err != nil {
		t.Fatal(err)
	}
	if _, f, err := s.DownloadAttachment(ctx, owner, draft.ID); err != nil {
		t.Fatal(err)
	} else {
		f.Close()
	}
	if _, _, err := s.DownloadAttachment(ctx, one, draft.ID); ErrorStatus(err) != 404 {
		t.Fatal("draft exposed", err)
	}
	in := StartChatInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "Check these orders.", AttachmentIDs: []string{draft.ID}}
	sent, err := s.StartChat(ctx, owner, in, "files-first")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.StartChat(ctx, owner, in, "files-first")
	if err != nil || again.Messages[0].ID != sent.Messages[0].ID {
		t.Fatal(again, err)
	}
	for _, a := range []Actor{owner, one, two} {
		chat, err := s.Conversation(ctx, a, sent.ConversationID)
		if err != nil || len(chat.Messages) != 1 || len(chat.Messages[0].Attachments) != 1 {
			t.Fatal(chat, err)
		}
		page, err := s.Query(ctx, a, QueryInput{Collection: "messages", Filters: map[string]string{"conversation_id": sent.ConversationID}})
		if err != nil || len(page.Items) != 1 || len(page.Items[0].(Message).Attachments) != 1 {
			t.Fatal(page, err)
		}
		files, err := s.Query(ctx, a, QueryInput{Collection: "attachments", Filters: map[string]string{"conversation_id": sent.ConversationID}})
		if err != nil || len(files.Items) != 1 || files.Items[0].(Attachment).MessageID != sent.Messages[0].ID {
			t.Fatal(files, err)
		}
		_, file, err := s.DownloadAttachment(ctx, a, draft.ID)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(file)
		file.Close()
		if string(raw) != "%PDF-1.4\norders 701 702" {
			t.Fatal("wrong file")
		}
	}
	if _, err = s.Send(ctx, owner, SendInput{Target: Target{"agent", one.ID}, Body: "Reusing is explicit.", AttachmentIDs: []string{draft.ID}}, "different-op"); ErrorStatus(err) != 400 {
		t.Fatal("bound draft reused", err)
	}
	inbox, err := s.Inbox(ctx, one)
	if err != nil || len(inbox) != 1 {
		t.Fatal(inbox, err)
	}
	m := inbox[0].Payload["message"].(map[string]any)
	if len(m["attachments"].([]any)) != 1 {
		t.Fatal("worker file reference missing", m)
	}
}
func TestDraftFilesRollbackAndPrivateFanout(t *testing.T) {
	s, owner := testStore(t)
	s.AttachmentsRoot = t.TempDir()
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	two := testWorker(t, s, owner, "Builder 2")
	draft, err := s.UploadAttachment(ctx, owner, "", "brief.txt", "text/plain", []byte("private brief"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, owner, SendInput{Target: Target{"agent", one.ID}, Body: "Rollback", AttachmentIDs: []string{draft.ID, "file_missing"}}, "rollback"); err == nil {
		t.Fatal("missing draft accepted")
	}
	var links int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM attachment_links WHERE scope=$1`, owner.Scope).Scan(&links); err != nil || links != 0 {
		t.Fatal(links, err)
	}
	result, err := s.SendBatch(ctx, owner, SendBatchInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "", AttachmentIDs: []string{draft.ID}}, "private-files")
	if err != nil || len(result) != 2 {
		t.Fatal(result, err)
	}
	for i, a := range []Actor{one, two} {
		chat, err := s.Conversation(ctx, a, result[i].Result.ConversationID)
		if err != nil || len(chat.Messages[0].Attachments) != 1 {
			t.Fatal(chat, err)
		}
		if _, err = s.Conversation(ctx, a, result[1-i].Result.ConversationID); ErrorStatus(err) != 404 {
			t.Fatal("private copy leaked", err)
		}
	}
}
func TestDraftFilesWithApprovalAndConcurrentClaims(t *testing.T) {
	s, owner := testStore(t)
	s.AttachmentsRoot = t.TempDir()
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	draft, err := s.UploadAttachment(ctx, owner, "", "plan.txt", "text/plain", []byte("migration plan"))
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.Ask(ctx, owner, AskInput{Target: Target{"agent", one.ID}, Kind: "approval", Body: "Approve this plan?", AttachmentIDs: []string{draft.ID}}, "approval-file")
	if err != nil || len(q.Attachments) != 1 {
		t.Fatal(q, err)
	}
	got, err := s.Request(ctx, one, q.ID)
	if err != nil || len(got.Attachments) != 1 {
		t.Fatal(got, err)
	}
	next, err := s.UploadAttachment(ctx, owner, "", "race.txt", "text/plain", []byte("claim once"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Send(ctx, owner, SendInput{Target: Target{"agent", one.ID}, Body: "Concurrent claim", AttachmentIDs: []string{next.ID}}, ID("key"))
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for e := range results {
		if e == nil {
			accepted++
		} else if ErrorStatus(e) != 400 {
			t.Fatal(e)
		}
	}
	if accepted != 1 {
		t.Fatal("draft claimed more than once", accepted)
	}
}
func TestUnsentDraftExpiresWithoutRemovingAcceptedFiles(t *testing.T) {
	s, owner := testStore(t)
	s.AttachmentsRoot = t.TempDir()
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	draft, err := s.UploadAttachment(ctx, owner, "", "abandoned.txt", "text/plain", []byte("abandoned"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE attachments SET draft_until=now()-interval '1 second' WHERE id=$1`, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE attachment_uploads SET lease_until=now()-interval '1 second',next_at=now() WHERE id=$1`, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, owner, SendInput{Target: Target{"agent", one.ID}, Body: "expired", AttachmentIDs: []string{draft.ID}}, "expired-file"); ErrorStatus(err) != 400 {
		t.Fatal(err)
	}
	if worked, err := s.ProcessAttachmentCleanup(ctx); err != nil || !worked {
		t.Fatal(worked, err)
	}
	if _, _, err := s.DownloadAttachment(ctx, owner, draft.ID); ErrorStatus(err) != 404 {
		t.Fatal("expired draft survived", err)
	}
}

func TestMessageFilesAndMembershipWindowsSurviveWorkspaceExport(t *testing.T) {
	s, owner := testStore(t)
	s.AttachmentsRoot = t.TempDir()
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	two := testWorker(t, s, owner, "Builder 2")
	file, err := s.UploadAttachment(ctx, owner, "", "release.txt", "text/plain", []byte("release evidence"))
	if err != nil {
		t.Fatal(err)
	}
	sent, err := s.StartChat(ctx, owner, StartChatInput{Targets: []Target{{"agent", one.ID}}, Body: "Check this file", AttachmentIDs: []string{file.ID}}, "export-chat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChangeChatMembership(ctx, owner, sent.ConversationID, ChatMembershipInput{Operation: "add", Version: 1, Members: []string{two.ID}}, "export-member"); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err = s.ExportWorkspace(ctx, owner.Scope, &archive); err != nil {
		t.Fatal(err)
	}
	files := archiveFiles(t, archive.Bytes())
	if files["attachments/"+file.ID] != "release evidence" || !strings.Contains(files["tables/attachment_links.ndjson"], sent.Messages[0].ID) || !strings.Contains(files["tables/participants.ndjson"], `"history_start"`) {
		t.Fatal("export lost the file's message association or membership window")
	}
}
