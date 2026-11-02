package engine

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"os"
	"sync"
	"testing"
)

func TestOpenExistingPreservesConnectionFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store, err := OpenExisting(ctx, "postgres://unused@127.0.0.1/beepgrid?sslmode=disable", "startup_test", nil)
	if store != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("connection failure was hidden by a migration error: store=%v err=%v", store, err)
	}
}

func TestUpgradeConcurrentMigrationsAndRestartRecovery(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	url := os.Getenv("BEEPGRID_TEST_DATABASE_URL")
	name := s.Pool.Config().ConnConfig.RuntimeParams["search_path"]
	name = name[:len(name)-7]
	w := testWorker(t, s, a, "restart-worker")
	sent, e := s.Send(ctx, a, SendInput{Target: Target{"agent", w.ID}, Body: "Durable across restart"}, "restart")
	if e != nil {
		t.Fatal(e)
	}
	jobs, e := s.Inbox(ctx, w)
	if e != nil || len(jobs) != 1 {
		t.Fatal(jobs, e)
	}
	stale := jobs[0]
	s.Close()
	again, e := OpenExisting(ctx, url, name, []byte("01234567890123456789012345678901"))
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _, _ = again.Pool.Exec(ctx, `DROP SCHEMA `+name+` CASCADE`); again.Close() }()
	if _, e = again.Conversation(ctx, w, sent.ConversationID); e != nil {
		t.Fatal(e)
	}
	_, e = again.Pool.Exec(ctx, `UPDATE deliveries SET lease_until=now()-interval '1 second'`)
	if e != nil {
		t.Fatal(e)
	}
	leased, e := again.Inbox(ctx, w)
	if e != nil || len(leased) != 1 || leased[0].Generation <= stale.Generation {
		t.Fatal(leased, e)
	}
	if e = again.Ack(ctx, w, stale.ID, stale.Generation); ErrorStatus(e) != 409 {
		t.Fatal("stale restart ack", e)
	}
	if e = again.Ack(ctx, w, leased[0].ID, leased[0].Generation); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := Migrate(ctx, url, name); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	// Simulate an old v1 install in another isolated schema and upgrade it.
	oldName := "upgrade_" + Digest(name)[:20]
	old, e := pool(ctx, url, oldName)
	if e != nil {
		t.Fatal(e)
	}
	defer old.Close()
	defer old.Exec(ctx, `DROP SCHEMA `+oldName+` CASCADE`)
	if _, e = old.Exec(ctx, `CREATE SCHEMA `+oldName); e != nil {
		t.Fatal(e)
	}
	raw, _ := migrations.ReadFile("migrations/001.sql")
	if _, e = old.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	if _, e = old.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES(1)`); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, url, oldName); e != nil {
		t.Fatal(e)
	}
	var version int
	_ = old.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version)
	if version != SchemaVersion {
		t.Fatal(version)
	}
	// Recovery rotates tokens and prevents new effects until an operator resumes.
	newToken := Secret()
	if e = again.ReconcileRestored(ctx, a.Scope, newToken); e != nil {
		t.Fatal(e)
	}
	if _, e = again.Authenticate(ctx, newToken); e != nil {
		t.Fatal(e)
	}
	if _, e = again.Send(ctx, a, SendInput{Target: Target{"agent", w.ID}, Body: "Blocked"}, "held"); ErrorStatus(e) != 503 {
		t.Fatal("restore hold", e)
	}
	if e = again.ResumeAfterReconciliation(ctx, a.Scope); e != nil {
		t.Fatal(e)
	}
	if e = again.tx(ctx, w, func(_ pgx.Tx) error { return nil }); ErrorStatus(e) != 403 {
		t.Fatal("restored credential stayed valid", e)
	}
}
