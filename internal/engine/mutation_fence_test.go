package engine

import (
	"context"
	"testing"
	"time"
)

func TestMutationFenceObservesRevocationAfterReplicaLockWait(t *testing.T) {
	for _, kind := range []string{"revoked", "disabled", "suspended", "recovery_hold"} {
		t.Run(kind, func(t *testing.T) {
			s, owner := testStore(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			worker := testWorker(t, s, owner, "fenced")
			held, err := s.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Rollback(ctx)
			if _, err = held.Exec(ctx, `SELECT id FROM scopes WHERE id=$1 FOR UPDATE`, owner.Scope); err != nil {
				t.Fatal(err)
			}
			// A distinct Store represents another replica, with its own admission gate.
			replica := &Store{Pool: s.Pool, SecretKey: s.SecretKey}
			result := make(chan error, 1)
			go func() {
				_, e := replica.Send(ctx, worker, SendInput{Target: Target{"person", owner.ID}, Body: "Must never be committed"}, "blocked")
				result <- e
			}()
			blocked := false
			for !blocked && ctx.Err() == nil {
				if err = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, held.Conn().PgConn().PID()).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if !blocked {
					time.Sleep(5 * time.Millisecond)
				}
			}
			if !blocked {
				t.Fatal("second replica never waited for fence")
			}
			query := map[string]string{"revoked": `UPDATE credentials SET revoked=true WHERE id=$1`, "disabled": `UPDATE principals SET disabled=true WHERE id=$1`, "suspended": `UPDATE scopes SET suspended=true WHERE id=$1`, "recovery_hold": `UPDATE scopes SET recovery_hold=true WHERE id=$1`}[kind]
			id := worker.CredentialID
			if kind == "disabled" {
				id = worker.ID
			}
			if kind == "suspended" || kind == "recovery_hold" {
				id = owner.Scope
			}
			if _, err = held.Exec(ctx, query, id); err != nil {
				t.Fatal(err)
			}
			if err = held.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			want := 403
			if kind == "suspended" || kind == "recovery_hold" {
				want = 503
			}
			if err = <-result; ErrorStatus(err) != want {
				t.Fatal("stale authorization after fence", err)
			}
			var count int
			if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE scope=$1`, owner.Scope).Scan(&count); err != nil || count != 0 {
				t.Fatal("denied mutation persisted", count, err)
			}
		})
	}
}
