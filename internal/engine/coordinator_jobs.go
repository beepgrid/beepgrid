package engine

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

// ProcessCoordinatorSummary leases one ready job. The indexed queue considers
// only automatic plans with durable dispatches, never polls individual agents.
// Model execution stays outside transactions. A crash releases the lease after
// one minute; cached summaries and digest checks make recovery safe.
func (s *Store) ProcessCoordinatorSummary(ctx context.Context) (int, error) {
	if s.OverseerSummarizer == nil {
		return 0, nil
	}
	var id, scope, person, credential string
	var lease time.Time
	err := s.Pool.QueryRow(ctx, `WITH candidate AS (
 SELECT p.id FROM overseer_plans p WHERE p.automatic AND p.result IS NOT NULL AND NOT p.summary_done AND p.summary_next_at<=now() AND (p.summary_lease_until IS NULL OR p.summary_lease_until<=now())
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(p.result) d WHERE COALESCE(d->>'request_id','')<>'')
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p.result) d JOIN requests r ON r.scope=p.scope AND r.id=d->>'request_id' WHERE r.state='open' AND r.expires_at>now())
 ORDER BY p.summary_next_at,p.id FOR UPDATE SKIP LOCKED LIMIT 1
 ) UPDATE overseer_plans p SET summary_lease_until=now()+interval '1 minute',summary_next_at=now()+interval '1 minute' FROM candidate c WHERE p.id=c.id RETURNING p.id,p.scope,p.person_id,p.authority_credential,p.summary_lease_until`).Scan(&id, &scope, &person, &credential, &lease)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	actor, err := s.RefreshActor(ctx, Actor{Scope: scope, CredentialID: credential})
	if err == nil && actor.ID != person {
		err = Denied()
	}
	if err == nil {
		_, err = s.SummarizeOverseer(ctx, actor, id)
	}
	if err != nil {
		tx, saveErr := s.Pool.Begin(ctx)
		if saveErr == nil {
			tag, updateErr := tx.Exec(ctx, `UPDATE overseer_plans SET summary_lease_until=NULL,summary_next_at=now()+interval '5 minutes',summary_error='Could not summarize replies. Reopen Coordinator to retry.' WHERE scope=$1 AND id=$2 AND summary_lease_until=$3`, scope, id, lease)
			saveErr = updateErr
			if saveErr == nil && tag.RowsAffected() > 0 {
				saveErr = event(ctx, tx, Actor{Scope: scope, Principal: Principal{ID: person, Kind: "person"}}, "overseer.summary_failed", "", person, map[string]string{"id": id})
			}
			if saveErr == nil {
				saveErr = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(ctx)
			}
		}
		if saveErr != nil {
			return 1, saveErr
		}
		return 1, err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE overseer_plans SET summary_done=true,summary_lease_until=NULL,summary_error='' WHERE scope=$1 AND id=$2 AND summary_lease_until=$3`, scope, id, lease)
	return 1, err
}

// RunCoordinatorSummaries has one worker per service, a bounded model call and
// no new infrastructure. Shutdown joins the in-flight work through its context.
func (s *Store) RunCoordinatorSummaries(ctx context.Context) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for ctx.Err() == nil {
		step, cancel := context.WithTimeout(ctx, 40*time.Second)
		n, err := s.ProcessCoordinatorSummary(step)
		cancel()
		if n > 0 && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
