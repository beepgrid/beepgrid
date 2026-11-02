package engine

import (
	"context"
	"io"
	"testing"
)

func TestAttachmentInheritsCurrentConversationAccess(t *testing.T) {
	s, a := testStore(t)
	s.AttachmentsRoot = t.TempDir()
	ctx := context.Background()
	w := testWorker(t, s, a, "worker")
	sent, e := s.Send(ctx, a, SendInput{Target: Target{"agent", w.ID}, Body: "Review evidence"}, "attachment-chat")
	if e != nil {
		t.Fatal(e)
	}
	x, e := s.UploadAttachment(ctx, a, sent.ConversationID, "../report.txt", "text/plain", []byte("Evidence"))
	if e != nil {
		t.Fatal(e)
	}
	if x.FileName != "report.txt" {
		t.Fatal(x)
	}
	_, f, e := s.DownloadAttachment(ctx, w, x.ID)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := io.ReadAll(f)
	f.Close()
	if string(raw) != "Evidence" {
		t.Fatal("content mismatch")
	}
	outsider := testPerson(t, s, a, "outsider")
	if _, _, e = s.DownloadAttachment(ctx, outsider, x.ID); ErrorStatus(e) != 404 {
		t.Fatal("private attachment leaked", e)
	}
	if _, e = s.UploadAttachment(ctx, a, sent.ConversationID, "attack.html", "text/html", []byte("<script>")); ErrorStatus(e) != 400 {
		t.Fatal("active content accepted", e)
	}
}
