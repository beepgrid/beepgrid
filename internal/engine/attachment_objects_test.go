package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type seekContent struct{ *bytes.Reader }

func (seekContent) Close() error { return nil }

type objectFixture struct {
	data     map[string][]byte
	fail     bool
	afterPut func()
	opens    int
	deletes  int
}

func (m *objectFixture) Put(_ context.Context, k string, b []byte) error {
	if m.fail {
		return errors.New("offline")
	}
	m.data[k] = append([]byte(nil), b...)
	if m.afterPut != nil {
		m.afterPut()
	}
	return nil
}
func (m *objectFixture) Open(_ context.Context, k string) (io.ReadSeekCloser, error) {
	m.opens++
	b, ok := m.data[k]
	if !ok {
		return nil, errors.New("missing")
	}
	return seekContent{bytes.NewReader(b)}, nil
}
func (m *objectFixture) Delete(_ context.Context, k string) error {
	m.deletes++
	delete(m.data, k)
	return nil
}

func TestObjectAttachmentsIsolationIntegrityAndFailures(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "worker")
	m := &objectFixture{data: map[string][]byte{}}
	s.AttachmentObjects = m
	chat, err := s.Send(ctx, a, SendInput{Target: Target{"agent", w.ID}, Body: "files"}, "files")
	if err != nil {
		t.Fatal(err)
	}
	m.fail = true
	if _, err = s.UploadAttachment(ctx, a, chat.ConversationID, "x.txt", "text/plain", []byte("data")); err == nil {
		t.Fatal("failed put accepted")
	}
	xs, _ := s.Attachments(ctx, a, chat.ConversationID)
	if len(xs) != 0 {
		t.Fatal("metadata before content")
	}
	m.fail = false
	x, err := s.UploadAttachment(ctx, a, chat.ConversationID, "x.txt", "text/plain", []byte("data"))
	if err != nil {
		t.Fatal(err)
	}
	key := attachmentObjectKey(a.Scope, x.ID)
	if strings.Contains(key, a.Scope) || m.data[key] == nil {
		t.Fatal("bad object namespace")
	}
	other, err := s.Bootstrap(ctx, "other/dev", "Other", "dev", Secret())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.DownloadAttachment(ctx, other, x.ID); err == nil || m.opens != 0 {
		t.Fatal("cross-tenant backend access")
	}
	_, file, err := s.DownloadAttachment(ctx, w, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(file)
	file.Close()
	if string(raw) != "data" {
		t.Fatal(string(raw))
	}
	m.data[key] = []byte("corrupt")
	if _, _, err = s.DownloadAttachment(ctx, w, x.ID); err == nil {
		t.Fatal("corrupt content exposed")
	}
	m.afterPut = func() {
		if err := s.Revoke(ctx, a, w.CredentialID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.UploadAttachment(ctx, w, chat.ConversationID, "x.txt", "text/plain", []byte("raced")); err == nil {
		t.Fatal("revoked upload accepted")
	}
	if m.deletes != 1 {
		t.Fatal("uncommitted object not cleaned", m.deletes)
	}
}
