package engine

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func archiveFiles(t *testing.T, b []byte) map[string]string {
	t.Helper()
	r := tar.NewReader(bytes.NewReader(b))
	out := map[string]string{}
	for {
		h, e := r.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		raw, e := io.ReadAll(r)
		if e != nil {
			t.Fatal(e)
		}
		out[h.Name] = string(raw)
	}
	return out
}
func TestWorkspaceExportSnapshotSecretsAndTenantIsolation(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	s.AttachmentsRoot = t.TempDir()
	w := testWorker(t, s, a, "export-worker")
	b, e := s.Bootstrap(ctx, "other/export", "Other private company", "export", Secret())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreatePerson(ctx, b, CreatePerson{ExternalID: "private-person", Name: "Never export this other tenant"}); e != nil {
		t.Fatal(e)
	}
	chat, e := s.Send(ctx, a, SendInput{Target: Target{"agent", w.ID}, Body: "Customer's canonical message"}, "export-chat")
	if e != nil {
		t.Fatal(e)
	}
	attachment, e := s.UploadAttachment(ctx, a, chat.ConversationID, "proof.txt", "text/plain", []byte("Private attachment proof"))
	if e != nil {
		t.Fatal(e)
	}
	var digest string
	if e = s.Pool.QueryRow(ctx, "SELECT digest FROM credentials WHERE scope=$1 LIMIT 1", a.Scope).Scan(&digest); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	writer := &snapshotExportWriter{Writer: &out, once: func() {
		if _, err := s.Send(ctx, a, SendInput{ConversationID: chat.ConversationID, Body: "Written after snapshot started"}, "after-export-snapshot"); err != nil {
			t.Fatal(err)
		}
	}}
	if e = s.ExportWorkspace(ctx, a.Scope, writer); e != nil {
		t.Fatal(e)
	}
	files := archiveFiles(t, out.Bytes())
	if files["complete.json"] != `{"complete":true}` || files["attachments/"+attachment.ID] != "Private attachment proof" {
		t.Fatal("incomplete archive", files)
	}
	if !strings.Contains(files["tables/messages.ndjson"], "Customer's canonical message") {
		t.Fatal("missing canonical history")
	}
	if strings.Contains(files["tables/messages.ndjson"], "Written after snapshot started") {
		t.Fatal("snapshot mixed concurrent writes")
	}
	for _, secret := range []string{digest, "Never export this other tenant", b.Scope} {
		if bytes.Contains(out.Bytes(), []byte(secret)) {
			t.Fatal("export crossed tenant or secret boundary")
		}
	}
	if _, ok := files["tables/credentials.ndjson"]; ok {
		t.Fatal("credential export")
	}
	if _, ok := files["tables/idempotency.ndjson"]; ok {
		t.Fatal("credential receipt export")
	}
	// Partial writes cannot produce a complete archive, and release the snapshot.
	if e = s.ExportWorkspace(ctx, a.Scope, failExportWriter{}); e == nil {
		t.Fatal("write failure ignored")
	}
	if e = os.WriteFile(s.attachmentPath(a.Scope, attachment.ID), []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.ExportWorkspace(ctx, a.Scope, io.Discard); e == nil {
		t.Fatal("corrupt attachment exported")
	}
	if _, e = s.Pool.Exec(ctx, `CREATE TABLE lifecycle_unknown(scope text NOT NULL, payload text)`); e != nil {
		t.Fatal(e)
	}
	if e = s.ExportWorkspace(ctx, a.Scope, io.Discard); e == nil {
		t.Fatal("unknown table silently omitted")
	}
}

type failExportWriter struct{}

type snapshotExportWriter struct {
	io.Writer
	once func()
}

func (w *snapshotExportWriter) Write(p []byte) (int, error) {
	if w.once != nil {
		f := w.once
		w.once = nil
		f()
	}
	return w.Writer.Write(p)
}

func (failExportWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

type failingDeleteObjects struct {
	objectFixture
	deny bool
}

func (s *failingDeleteObjects) Delete(ctx context.Context, key string) error {
	if s.deny {
		return errors.New("backend down")
	}
	return s.objectFixture.Delete(ctx, key)
}

func TestWorkspaceErasureRetentionFailureRecoveryAndTombstone(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "erase-worker")
	b, e := s.Bootstrap(ctx, "other/erase", "Other", "erase", Secret())
	if e != nil {
		t.Fatal(e)
	}
	m := &failingDeleteObjects{objectFixture: objectFixture{data: map[string][]byte{}}}
	s.AttachmentObjects = m
	chat, e := s.Send(ctx, a, SendInput{Target: Target{"agent", w.ID}, Body: "Erase my history"}, "erase-chat")
	if e != nil {
		t.Fatal(e)
	}
	attachment, e := s.UploadAttachment(ctx, a, chat.ConversationID, "proof.txt", "text/plain", []byte("Erase proof"))
	if e != nil {
		t.Fatal(e)
	}
	group, e := s.SaveGroup(ctx, a, Group{Name: "Erasure approvers", Members: []string{a.ID}})
	if e != nil {
		t.Fatal(e)
	}
	action := Action{Name: "simulation", ExecutorID: w.ID, Resource: "fixture", ResourceVersion: "v1", OperationKey: "erase-simulation"}
	approval, e := s.Ask(ctx, w, AskInput{Target: Target{"group", group.ID}, Kind: "approval", Title: "Retained approval", Action: &action}, "erase-approval")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, a, approval.ID, Response{Outcome: "approved", Version: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Redeem(ctx, w, approval.ID, action); e != nil {
		t.Fatal(e)
	}
	question, e := s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "Reusable convention", SharedKey: "fixture/v1"}, "erase-question")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, a, question.ID, Response{Outcome: "answered", Answer: "A scoped answer", Version: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishAnswer(ctx, a, PublishInput{RequestID: question.ID, Audience: []string{w.ID}}); e != nil {
		t.Fatal(e)
	}
	op := strings.Repeat("a", 32)
	due := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	if e = s.ReserveWorkspaceErasure(ctx, a.Scope, op, 10, due); e != nil {
		t.Fatal(e)
	}
	if e = s.ReserveWorkspaceErasure(ctx, a.Scope, op, 10, due); e != nil {
		t.Fatal("reservation replay", e)
	}
	if e = s.ReserveWorkspaceErasure(ctx, a.Scope, strings.Repeat("b", 32), 11, due); ErrorStatus(e) != 409 {
		t.Fatal("conflicting operation", e)
	}
	if done, e := s.EraseWorkspaceStep(ctx, a.Scope, op); e != nil || done {
		t.Fatal("retention ignored", done, e)
	}
	if e = s.ApplyWorkspaceState(ctx, a.Scope, 11, false); ErrorStatus(e) != 409 {
		t.Fatal("erasure was reactivated", e)
	}
	if _, e = s.Send(ctx, a, SendInput{ConversationID: chat.ConversationID, Body: "cached actor"}, "late"); e == nil {
		t.Fatal("cached actor wrote after reservation")
	}
	if e = s.RecordProviderEvent(ctx, a.Scope, "late", "late"); e == nil {
		t.Fatal("late provider receipt resurrected data")
	}
	if _, e = s.Me(ctx, b); e != nil {
		t.Fatal("other tenant affected", e)
	}
	if e = s.ExportWorkspace(ctx, a.Scope, io.Discard); e != nil {
		t.Fatal("retention-period export", e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE scopes SET erase_after=now()-interval '1 second' WHERE id=$1`, a.Scope); e != nil {
		t.Fatal(e)
	}
	m.deny = true
	if _, e = s.EraseWorkspaceStep(ctx, a.Scope, op); e == nil {
		t.Fatal("failed object erasure acknowledged")
	}
	var n int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE scope=$1`, a.Scope).Scan(&n); e != nil || n != 1 {
		t.Fatal("lost retry metadata", n, e)
	}
	if e = s.ExportWorkspace(ctx, a.Scope, io.Discard); ErrorStatus(e) != 409 {
		t.Fatal("partial-erasure export", e)
	}
	m.deny = false
	if done, e := s.EraseWorkspaceStep(ctx, a.Scope, op); e != nil || done {
		t.Fatal("object batch", done, e)
	}
	if m.data[attachmentObjectKey(a.Scope, attachment.ID)] != nil {
		t.Fatal("object survived")
	}
	if done, e := s.EraseWorkspaceStep(ctx, a.Scope, op); e != nil || !done {
		t.Fatal("final deletion", done, e)
	}
	if done, e := s.EraseWorkspaceStep(ctx, a.Scope, op); e != nil || !done {
		t.Fatal("lost completion replay", done, e)
	}
	for _, table := range lifecycleTables {
		if e = s.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE scope=$1", a.Scope).Scan(&n); e != nil || n != 0 {
			t.Fatal("tenant residue", table, n, e)
		}
	}
	var company string
	if e = s.Pool.QueryRow(ctx, `SELECT company_name FROM scopes WHERE id=$1`, a.Scope).Scan(&company); e != nil || company != "" {
		t.Fatal("company not erased", e)
	}
	if _, e = s.Me(ctx, b); e != nil {
		t.Fatal("other tenant erased", e)
	}
	if e = s.ApplyWorkspaceAccess(ctx, a.Scope, 100, false, time.Now().Add(time.Hour)); ErrorStatus(e) != 409 {
		t.Fatal("tombstone revived", e)
	}
}

func TestErasureLocksOnlyItsWorkspaceAndRejectsUnrecognizedTables(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	b, e := s.Bootstrap(ctx, "other/locks", "Other", "locks", Secret())
	if e != nil {
		t.Fatal(e)
	}
	_, release, e := s.lifecycleConnection(ctx, a.Scope, true)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	op := strings.Repeat("c", 32)
	if e = s.ReserveWorkspaceErasure(ctx, a.Scope, op, 2, time.Now().Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.EraseWorkspaceStep(ctx, a.Scope, op); ErrorStatus(e) != 409 {
		t.Fatal("export and erase raced", e)
	}
	if e = s.ExportWorkspace(ctx, b.Scope, io.Discard); e != nil {
		t.Fatal("unrelated export locked", e)
	}
	release()
	release = nil
	if _, e = s.Pool.Exec(ctx, `CREATE TABLE unknown_customer_data(scope text, body text)`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.EraseWorkspaceStep(ctx, a.Scope, op); e == nil {
		t.Fatal("unknown table silently left behind")
	}
	status, e := s.WorkspaceErasure(ctx, a.Scope)
	if e != nil || status.Completed != nil {
		t.Fatal("false completion", status, e)
	}
}
