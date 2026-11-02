package cli

import (
	"context"
	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/internal/httpapi"
	"github.com/beepgrid/beepgrid/sdk/go/notif"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedCreatesRestrictedLauncherWithoutPrintingItsToken(t *testing.T) {
	url := os.Getenv("NOTIF_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("Set NOTIF_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	schema := "seed_" + engine.Digest(engine.Secret())[:20]
	s, e := engine.Open(ctx, url, schema, []byte("01234567890123456789012345678901"))
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _, _ = s.Pool.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); s.Close() }()
	token := engine.Secret()
	a, e := s.Bootstrap(ctx, "test/dev", "Test", "dev", token)
	if e != nil {
		t.Fatal(e)
	}
	h := httpapi.New(s, "http://localhost", nil)
	server := httptest.NewServer(h)
	defer server.Close()
	h.PublicURL = server.URL
	t.Setenv("NOTIF_URL", server.URL)
	t.Setenv("NOTIF_TOKEN", token)
	out := filepath.Join(t.TempDir(), "launcher-token")
	if e = seed(ctx, []string{"--out", out, "--prefix", "team/"}); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(out)
	if e != nil {
		t.Fatal(e)
	}
	info, _ := os.Stat(out)
	if info.Mode().Perm() != 0600 {
		t.Fatal("unsafe credential permissions")
	}
	launcher := notif.New(server.URL, strings.TrimSpace(string(raw)))
	worker, session, e := launcher.Enroll(ctx, notif.EnrollInput{Agent: "team/scout", Name: "Scout"})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = launcher.Enroll(ctx, notif.EnrollInput{Agent: "other/scout", Name: "Other"}); e == nil {
		t.Fatal("namespace escaped")
	}
	if _, e = worker.Ask(ctx, notif.AskInput{Target: notif.Target{Kind: "person", ID: a.ID}, Title: "Allowed?"}, "seed-test"); e != nil {
		t.Fatal(e)
	}
	if session.Agent.ID == a.ID {
		t.Fatal("people/agents merged")
	}
	if e = seed(ctx, []string{"--out", out}); e == nil {
		t.Fatal("seed overwrote credential")
	}
}
