package runtime

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/notif-dev/notif/internal/engine"
	"github.com/notif-dev/notif/sdk/go/notif"
)

func service(t *testing.T) (*Service, Config) {
	t.Helper()
	db := os.Getenv("NOTIF_TEST_DATABASE_URL")
	if db == "" {
		t.Skip("PostgreSQL is required")
	}
	c := Config{DatabaseURL: db, Schema: "runtime_" + strings.TrimPrefix(engine.ID(""), "_"), PublicURL: "http://localhost", SecretKey: []byte("01234567890123456789012345678901"), MaxConnections: 4}
	if err := Migrate(context.Background(), c.DatabaseURL, c.Schema); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.store.Pool.Exec(context.Background(), "DROP SCHEMA "+c.Schema+" CASCADE"); s.Close() })
	return s, c
}

func TestSharedRuntimeTenantIsolationAndSuspension(t *testing.T) {
	s, c := service(t)
	stats, err := s.Workload(context.Background())
	if err != nil || stats.Queued != 0 || stats.DatabaseConnections > 4 {
		t.Fatal(stats, err)
	}
	ctx := context.Background()
	tokenA, tokenB := engine.Secret(), engine.Secret()
	if err := s.ProvisionWorkspace(ctx, "tenant_a", "A", tokenA); err != nil {
		t.Fatal(err)
	}
	if err := s.ProvisionWorkspace(ctx, "tenant_b", "B", tokenB); err != nil {
		t.Fatal(err)
	}
	if err := s.ProvisionWorkspace(ctx, "tenant_a", "A", tokenA); err != nil {
		t.Fatal("replay", err)
	}
	if err := s.ProvisionWorkspace(ctx, "tenant_a", "A", tokenB); err == nil {
		t.Fatal("credential hijack")
	}
	if err := s.ProvisionWorkspace(ctx, "tenant_a", "Changed", tokenA); err == nil {
		t.Fatal("conflicting replay")
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	a, b := notif.New(server.URL, tokenA), notif.New(server.URL, tokenB)
	var person struct {
		Person notif.Person `json:"person"`
	}
	if err := a.Do(ctx, "GET", "/api/v1/me", nil, &person, ""); err != nil {
		t.Fatal(err)
	}
	_, session, err := b.Enroll(ctx, notif.EnrollInput{Agent: "same-name", Name: "Scout"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Send(ctx, notif.SendInput{Target: notif.Target{Kind: "agent", ID: session.Agent.ID}, Body: "cross tenant"}, "cross"); err == nil {
		t.Fatal("cross-tenant target accepted")
	}
	if _, err = b.Ask(ctx, notif.AskInput{Target: notif.Target{Kind: "person", ID: person.Person.ID}, Title: "Approve?", Choices: []string{"Yes"}}, "cross"); err == nil {
		t.Fatal("cross-tenant approval accepted")
	}
	if err = s.SetSuspended(ctx, "tenant_a", true); err != nil {
		t.Fatal(err)
	}
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err == nil {
		t.Fatal("suspended credential works")
	}
	if err = b.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err != nil {
		t.Fatal("other tenant affected", err)
	}
	replica, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer replica.Close()
	if _, err = replica.store.Authenticate(ctx, tokenA); err == nil {
		t.Fatal("replica ignored suspension")
	}
	if err = s.SetSuspended(ctx, "tenant_a", false); err != nil {
		t.Fatal(err)
	}
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err != nil {
		t.Fatal("resume", err)
	}
	if err = s.ApplyWorkspaceState(ctx, "tenant_a", 20, true); err != nil {
		t.Fatal(err)
	}
	if err = s.ApplyWorkspaceState(ctx, "tenant_a", 19, false); err == nil {
		t.Fatal("stale command reactivated tenant")
	}
	if err = s.ApplyWorkspaceState(ctx, "tenant_a", 20, false); err == nil {
		t.Fatal("same-version payload conflict")
	}
	if err = s.ProvisionWorkspace(ctx, "tenant_a", "A", tokenA); err != nil {
		t.Fatal("suspended provisioning replay", err)
	}
	if err = a.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err == nil {
		t.Fatal("replay removed suspension")
	}
	if err = s.ApplyWorkspaceState(ctx, "tenant_a", 21, false); err != nil {
		t.Fatal(err)
	}
	if s.store.Pool.Config().MaxConns != 4 {
		t.Fatal("pool budget ignored")
	}
}

func TestWorkersJoinAndPoolBounds(t *testing.T) {
	s, c := service(t)
	for _, bad := range []int32{1, 101} {
		c.MaxConnections = bad
		if _, err := Open(context.Background(), c); err == nil {
			t.Fatal("invalid pool", bad)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.RunWorkers(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("workers did not drain")
	}
}
