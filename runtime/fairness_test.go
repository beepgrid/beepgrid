package runtime

import (
	"context"
	"fmt"
	"github.com/beepgrid/beepgrid/internal/engine"
	sdk "github.com/beepgrid/beepgrid/sdk/go/notif"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"testing"
	"time"
)

// A stalled workspace must not occupy every pooled connection while its peers
// wait on the same row lock. This models a slow transaction in one tenant,
// while another tenant continues ordinary authenticated communication.
func TestBusyWorkspaceCannotExhaustSharedPoolWhileWaitingOnItsOwnLock(t *testing.T) {
	for _, budget := range []int32{2, 4} {
		t.Run(fmt.Sprint("pool-", budget), func(t *testing.T) { busyWorkspace(t, budget) })
	}
}

func busyWorkspace(t *testing.T, budget int32) {
	service, c := serviceWithBudget(t, budget)
	ctx := context.Background()
	tokens := []string{engine.Secret(), engine.Secret()}
	for i, id := range []string{"tenant_busy", "tenant_quiet"} {
		if e := service.ProvisionWorkspace(ctx, id, id, tokens[i]); e != nil {
			t.Fatal(e)
		}
	}
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	busy, quiet := sdk.New(server.URL, tokens[0]), sdk.New(server.URL, tokens[1])
	targets := []string{}
	for _, client := range []*sdk.Client{busy, quiet} {
		_, session, e := client.Enroll(ctx, sdk.EnrollInput{Agent: "worker", Name: "Worker"})
		if e != nil {
			t.Fatal(e)
		}
		targets = append(targets, session.Agent.ID)
	}
	blocker, e := pgxpool.New(ctx, c.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Close()
	tx, e := blocker.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT id FROM "+pgx.Identifier{c.Schema, "scopes"}.Sanitize()+" WHERE id='tenant_busy/production' FOR UPDATE"); e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func(i int) {
			request, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			_, e := busy.Send(request, sdk.SendInput{Target: sdk.Target{Kind: "agent", ID: targets[0]}, Body: "Busy tenant's accepted work"}, fmt.Sprintf("fair-busy-%d", i))
			results <- e
		}(i)
	}
	until := time.Now().Add(2 * time.Second)
	for service.store.Pool.Stat().AcquiredConns() == 0 && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	occupied := service.store.Pool.Stat().AcquiredConns()
	request, cancel := context.WithTimeout(ctx, 2*time.Second)
	started := time.Now()
	_, quietErr := quiet.Send(request, sdk.SendInput{Target: sdk.Target{Kind: "agent", ID: targets[1]}, Body: "Unrelated tenant remains responsive"}, "fair-quiet")
	latency := time.Since(started)
	cancel()
	// Release the controlled stall before reporting errors or joining all callers.
	if e = tx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 16; i++ {
		if e = <-results; e != nil {
			t.Error("busy request failed after stall released", e)
		}
	}
	t.Logf("waiting_connections=%d pool_limit=%d unrelated_tenant_latency=%s", occupied, c.MaxConnections, latency)
	if quietErr != nil {
		t.Fatal("one tenant starved unrelated communication", quietErr)
	}
}
