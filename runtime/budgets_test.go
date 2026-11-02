package runtime

import (
	"context"
	"errors"
	"fmt"
	"github.com/beepgrid/beepgrid/internal/engine"
	sdk "github.com/beepgrid/beepgrid/sdk/go"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestBudgetsAcrossReplicasRetryRollbackAndIsolation(t *testing.T) {
	s, c := service(t)
	ctx := context.Background()
	tokenA, tokenB := engine.Secret(), engine.Secret()
	for id, token := range map[string]string{"budget_a": tokenA, "budget_b": tokenB} {
		if e := s.ProvisionWorkspace(ctx, id, id, token); e != nil {
			t.Fatal(e)
		}
	}
	replica, e := Open(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	defer replica.Close()
	one, two := httptest.NewServer(s.Handler()), httptest.NewServer(replica.Handler())
	defer one.Close()
	defer two.Close()
	clients := []*sdk.Client{sdk.New(one.URL, tokenA), sdk.New(two.URL, tokenA)}
	_, session, e := clients[0].Enroll(ctx, sdk.EnrollInput{Agent: "worker", Name: "Worker"})
	if e != nil {
		t.Fatal(e)
	}
	input := sdk.SendInput{Target: sdk.Target{Kind: "agent", ID: session.Agent.ID}, Body: "durable work"}
	b := WorkspaceBudget{Version: 1, Messages: 25, Requests: 0, UploadBytes: 0}
	if e = s.ApplyWorkspaceBudget(ctx, "budget_a", b); e != nil {
		t.Fatal(e)
	}
	if e = replica.ApplyWorkspaceBudget(ctx, "budget_a", b); e != nil {
		t.Fatal("replay", e)
	}
	conflict := b
	conflict.Messages = 26
	if e = s.ApplyWorkspaceBudget(ctx, "budget_a", conflict); ErrorStatus(e) != 409 {
		t.Fatal("version conflict", e)
	}
	var accepted, limited atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := clients[i%2].Send(ctx, input, fmt.Sprintf("budget-%d", i))
			if err == nil {
				accepted.Add(1)
			} else {
				var api *sdk.APIError
				if !errors.As(err, &api) || api.Status != 429 || api.Code != "QUOTA_EXCEEDED" {
					t.Error("unexpected failure", err)
				} else {
					limited.Add(1)
				}
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 25 || limited.Load() != 35 {
		t.Fatalf("accepted=%d limited=%d", accepted.Load(), limited.Load())
	}
	usage, e := s.WorkspaceUsage(ctx, "budget_a")
	if e != nil || len(usage) != 1 || usage[0].Units != 25 {
		t.Fatal(usage, e)
	}
	var first string
	if e = s.store.Pool.QueryRow(ctx, `SELECT key FROM idempotency WHERE scope='budget_a/production' AND operation='send' LIMIT 1`).Scan(&first); e != nil {
		t.Fatal(e)
	}
	if _, e = clients[1].Send(ctx, input, first); e != nil {
		t.Fatal("accepted retry at quota", e)
	}
	// Directly verify no canonical partial writes from the 35 rejected sends.
	var count int
	if e = s.store.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE scope='budget_a/production'`).Scan(&count); e != nil || count != 25 {
		t.Fatal(count, e)
	}
	if _, e = clients[0].Ask(ctx, sdk.AskInput{Target: input.Target, Title: "No allowance", Choices: []string{"Yes"}}, "request-zero"); e == nil {
		t.Fatal("zero request allowance ignored")
	}
	other := sdk.New(one.URL, tokenB)
	_, worker, e := other.Enroll(ctx, sdk.EnrollInput{Agent: "worker", Name: "Other"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.Send(ctx, sdk.SendInput{Target: sdk.Target{Kind: "agent", ID: worker.Agent.ID}, Body: "unaffected"}, "other"); e != nil {
		t.Fatal("other tenant blocked", e)
	}
	b.Version = 2
	b.Messages = 26
	if e = s.ApplyWorkspaceBudget(ctx, "budget_a", b); e != nil {
		t.Fatal(e)
	}
	if _, e = clients[1].Send(ctx, input, "raised"); e != nil {
		t.Fatal("raised budget", e)
	}
	if e = s.ApplyWorkspaceBudget(ctx, "budget_a", WorkspaceBudget{Version: 1, Messages: -1, Requests: -1, UploadBytes: -1}); ErrorStatus(e) != 409 {
		t.Fatal("stale bypass", e)
	}
	// Database time, rather than an untrusted event timestamp, selects the month.
	if _, e = s.store.Pool.Exec(ctx, `UPDATE workspace_usage SET period=period-interval '1 month' WHERE scope='budget_a/production'`); e != nil {
		t.Fatal(e)
	}
	if _, e = clients[0].Send(ctx, input, "new-month"); e != nil {
		t.Fatal("month renewal", e)
	}
	usage, e = s.WorkspaceUsage(ctx, "budget_a")
	if e != nil || len(usage) != 2 {
		t.Fatal(usage, e)
	}
}
