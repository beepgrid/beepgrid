package engine

import (
	"context"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"testing"
)

func testStore(t testing.TB) (*Store, Actor) {
	t.Helper()
	url := os.Getenv("BEEPGRID_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("Set BEEPGRID_TEST_DATABASE_URL for PostgreSQL integration tests.")
	}
	schema := "test_" + strings.TrimPrefix(ID(""), "_")
	ctx := context.Background()
	s, e := Open(ctx, url, schema, []byte("01234567890123456789012345678901"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = s.Pool.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); s.Close() })
	// PostgreSQL notification hints span schemas; each concurrent fixture also
	// needs a unique workspace name to observe only its own event hints.
	a, e := s.Bootstrap(ctx, schema+"/development", "Acme", "development", Secret())
	if e != nil {
		t.Fatal(e)
	}
	return s, a
}
func TestPeopleAreSeparateFromAgents(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	p, e := s.CreatePerson(ctx, a, CreatePerson{ExternalID: "alice", Name: "Alice", Department: "Finance"})
	if e != nil {
		t.Fatal(e)
	}
	session, e := s.Enroll(ctx, a, EnrollInput{Agent: "research/scout", Name: "Scout", Capabilities: []string{"message"}})
	if e != nil {
		t.Fatal(e)
	}
	people, e := s.ListPrincipals(ctx, a, "person")
	if e != nil {
		t.Fatal(e)
	}
	agents, e := s.ListPrincipals(ctx, a, "agent")
	if e != nil {
		t.Fatal(e)
	}
	if len(people) != 2 || len(agents) != 1 || agents[0].ID != session.Agent.ID || p.Kind != "person" {
		t.Fatalf("people=%+v agents=%+v", people, agents)
	}
	if _, e = s.CreatePerson(ctx, a, CreatePerson{ExternalID: "bob", Name: "Bob", ManagerID: session.Agent.ID}); ErrorStatus(e) != 400 {
		t.Fatalf("agent cannot be human manager: %v", e)
	}
}
func TestScopedEnrollmentAndRevocation(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	p, e := s.CreatePerson(ctx, a, CreatePerson{ExternalID: "launcher", Name: "Launcher"})
	if e != nil {
		t.Fatal(e)
	}
	key, e := s.NewCredential(ctx, a, p.ID, "enrollment", "team/")
	if e != nil {
		t.Fatal(e)
	}
	launcher, e := s.Authenticate(ctx, key["token"].(string))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Enroll(ctx, launcher, EnrollInput{Agent: "other/agent", Name: "Fake"}); ErrorStatus(e) != 403 {
		t.Fatal(e)
	}
	session, e := s.Enroll(ctx, launcher, EnrollInput{Agent: "team/scout", Name: "Scout", Metadata: map[string]any{"role": "admin", "enrolled_by": a.ID}})
	if e != nil {
		t.Fatal(e)
	}
	worker, e := s.Authenticate(ctx, session.Token)
	if e != nil {
		t.Fatal(e)
	}
	if worker.Admin || worker.Metadata["enrolled_by"] != p.ID {
		t.Fatal("worker self-assigned authority")
	}
	if _, e = s.Enroll(ctx, worker, EnrollInput{Agent: "team/child", Name: "Child"}); ErrorStatus(e) != 403 {
		t.Fatal(e)
	}
	if e = s.Revoke(ctx, a, worker.CredentialID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, session.Token); ErrorStatus(e) != 401 {
		t.Fatal(e)
	}
	if e = s.tx(ctx, worker, func(_ pgx.Tx) error { return nil }); e == nil {
		t.Fatal("revoked actor reused")
	}
}

func TestManualManagerEditingRejectsCycles(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	p, e := s.CreatePerson(ctx, a, CreatePerson{ExternalID: "direct", Name: "Direct", ManagerID: a.ID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpdatePerson(ctx, a, a.ID, CreatePerson{Name: "Owner", ManagerID: p.ID}); ErrorStatus(e) != 400 {
		t.Fatal("manager cycle", e)
	}
	updated, e := s.UpdatePerson(ctx, a, p.ID, CreatePerson{Name: "Renamed", Department: "Finance"})
	if e != nil || updated.Name != "Renamed" || updated.Metadata["department"] != "Finance" {
		t.Fatal(updated, e)
	}
}
