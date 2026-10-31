package main

import (
	"context"
	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/internal/httpapi"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointRestartDoesNotDuplicateQuestionOrResult(t *testing.T) {
	url := os.Getenv("NOTIF_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("Set NOTIF_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	schema := "checkpoint_" + engine.Digest(engine.Secret())[:20]
	s, e := engine.Open(ctx, url, schema, []byte("01234567890123456789012345678901"))
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _, _ = s.Pool.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); s.Close() }()
	token := engine.Secret()
	a, e := s.Bootstrap(ctx, "test/dev", "Checkpoint tests", "dev", token)
	if e != nil {
		t.Fatal(e)
	}
	launcher, e := s.CreateService(ctx, a, "launcher", "Launcher")
	if e != nil {
		t.Fatal(e)
	}
	role, e := s.SaveRole(ctx, a, engine.Role{Name: "Example worker", Rules: []engine.Rule{{Action: "view", TargetKind: "person", TargetID: a.ID}, {Action: "ask", TargetKind: "person", TargetID: a.ID}, {Action: "message", TargetKind: "person", TargetID: a.ID}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, a, engine.Binding{RoleID: role.ID, SubjectKind: "principal", SubjectID: launcher.ID}, false); e != nil {
		t.Fatal(e)
	}
	key, e := s.NewCredential(ctx, a, launcher.ID, "enrollment", "examples/")
	if e != nil {
		t.Fatal(e)
	}
	h := httpapi.New(s, "http://localhost", nil)
	server := httptest.NewServer(h)
	defer server.Close()
	h.PublicURL = server.URL
	t.Setenv("NOTIF_URL", server.URL)
	t.Setenv("NOTIF_TOKEN", key["token"].(string))
	t.Setenv("NOTIF_PERSON_ID", a.ID)
	state := filepath.Join(t.TempDir(), "state.json")
	if e = run(ctx, []string{"--state", state, "--stop-after-question"}); e != nil {
		t.Fatal(e)
	}
	requests, e := s.Requests(ctx, a)
	if e != nil || len(requests) != 1 {
		t.Fatal(requests, e)
	}
	q := requests[0]
	if _, e = s.Respond(ctx, a, q.ID, engine.Response{Outcome: "answered", Answer: "Brief", Version: q.Version}); e != nil {
		t.Fatal(e)
	}
	if e = run(ctx, []string{"--state", state}); e != nil {
		t.Fatal(e)
	}
	if e = run(ctx, []string{"--state", state}); e != nil {
		t.Fatal(e)
	}
	var count int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM requests`).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate question", count)
	}
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE body LIKE 'Checkpoint workflow resumed%'`).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate result", count)
	}
}
