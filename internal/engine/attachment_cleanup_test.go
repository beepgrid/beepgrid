package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

type cleanupBackend struct {
	objectFixture
	deleteFailure bool
}

func (b *cleanupBackend) Delete(ctx context.Context, key string) error {
	if b.deleteFailure {
		return errors.New("temporarily unavailable")
	}
	return b.objectFixture.Delete(ctx, key)
}
func TestCrashedUploadCleanupRespectsLeasesHoldsRetriesAndAcceptedFiles(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "worker")
	backend := &cleanupBackend{objectFixture: objectFixture{data: map[string][]byte{}}}
	s.AttachmentObjects = backend
	chat, e := s.Send(ctx, owner, SendInput{Target: Target{"agent", worker.ID}, Body: "Retained data"}, "retained")
	if e != nil {
		t.Fatal(e)
	}
	accepted, e := s.UploadAttachment(ctx, owner, chat.ConversationID, "accepted.txt", "text/plain", []byte("accepted"))
	if e != nil {
		t.Fatal(e)
	}
	orphan := ID("file")
	key := attachmentObjectKey(owner.Scope, orphan)
	backend.data[key] = []byte("interrupted upload")
	if _, e = s.Pool.Exec(ctx, `INSERT INTO attachment_uploads(id,scope) VALUES($1,$2)`, orphan, owner.Scope); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessAttachmentCleanup(ctx); e != nil || worked {
		t.Fatal("live upload collected", worked, e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE attachment_uploads SET lease_until=now()-interval '1 second',next_at=now()`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE scopes SET recovery_hold=true WHERE id=$1`, owner.Scope); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessAttachmentCleanup(ctx); e != nil || worked {
		t.Fatal("restore hold ignored", worked, e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE scopes SET recovery_hold=false WHERE id=$1`, owner.Scope); e != nil {
		t.Fatal(e)
	}
	backend.deleteFailure = true
	if worked, e := s.ProcessAttachmentCleanup(ctx); !worked || e == nil {
		t.Fatal("failed delete lost its intent", worked, e)
	}
	if backend.data[key] == nil {
		t.Fatal("failed deletion changed content")
	}
	backend.deleteFailure = false
	if _, e = s.Pool.Exec(ctx, `UPDATE attachment_uploads SET next_at=now()`); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessAttachmentCleanup(ctx); !worked || e != nil {
		t.Fatal(worked, e)
	}
	if backend.data[key] != nil || backend.data[attachmentObjectKey(owner.Scope, accepted.ID)] == nil {
		t.Fatal("cleanup deleted the wrong content")
	}
	var reservations int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM attachment_uploads`).Scan(&reservations); e != nil || reservations != 0 {
		t.Fatal(reservations, e)
	}
	// Live uploads delay destructive erasure until the bounded lease has passed.
	next := ID("file")
	backend.data[attachmentObjectKey(owner.Scope, next)] = []byte("pending")
	if _, e = s.Pool.Exec(ctx, `INSERT INTO attachment_uploads(id,scope) VALUES($1,$2)`, next, owner.Scope); e != nil {
		t.Fatal(e)
	}
	op := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if e = s.ReserveWorkspaceErasure(ctx, owner.Scope, op, 2, time.Unix(0, 0)); e != nil {
		t.Fatal(e)
	}
	if done, e := s.EraseWorkspaceStep(ctx, owner.Scope, op); e != nil || done {
		t.Fatal("live upload lease was bypassed", done, e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE attachment_uploads SET lease_until=now()-interval '1 second'`); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 4; i++ {
		done, e := s.EraseWorkspaceStep(ctx, owner.Scope, op)
		if e != nil {
			t.Fatal(e)
		}
		if done {
			if len(backend.data) != 0 {
				t.Fatal("erasure left pending object content")
			}
			return
		}
	}
	t.Fatal("erasure did not finish")
}
