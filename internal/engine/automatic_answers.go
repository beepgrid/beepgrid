package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

type AutomaticMatch struct {
	AnswerID   string  `json:"answer_id"`
	Equivalent bool    `json:"equivalent"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}
type AutomaticMatcher func(context.Context, Request, []SharedAnswer) (AutomaticMatch, error)

func (s *Store) queueAutomaticAnswer(ctx context.Context, tx pgx.Tx, a Actor, r Request) error {
	if s.AutomaticMatcher == nil || r.Kind != "question" || r.SharedKey == "" || r.State != "open" {
		return nil
	}
	settings, err := s.assistanceSettings(ctx, tx, a.Scope)
	if err != nil {
		return err
	}
	if !settings.AutomaticAnswers {
		return nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO answer_match_jobs(scope,request_id,version,credential_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, a.Scope, r.ID, settings.Version, a.CredentialID); err != nil {
		return err
	}
	// A short attention window allows a shared answer to arrive before external alerts.
	// Native history remains immediately available; provider delivery recovers even if matching stops.
	_, err = tx.Exec(ctx, `UPDATE deliveries SET next_at=GREATEST(next_at,now()+interval '25 seconds') WHERE scope=$1 AND operation_id=$2 AND connection_id<>'' AND state='queued'`, a.Scope, r.ID)
	return err
}

// ProcessAutomaticAnswer claims one durable job. Model I/O never holds a workspace lock.
// A crashed worker's lease expires, while uncertain model output never changes a request.
func (s *Store) ProcessAutomaticAnswer(ctx context.Context) (bool, error) {
	if s.AutomaticMatcher == nil {
		return false, nil
	}
	var scope, id, lease string
	var version int64
	lease = ID("matchlease")
	err := s.Pool.QueryRow(ctx, `WITH candidate AS (
 SELECT j.scope,j.request_id FROM answer_match_jobs j JOIN scopes s ON s.id=j.scope
 WHERE s.automatic_answers AND NOT s.recovery_hold AND NOT s.suspended AND s.assistance_version=j.version
 AND (j.state='queued' OR (j.state='leased' AND j.lease_until<now()))
 ORDER BY j.created_at LIMIT 1 FOR UPDATE OF j SKIP LOCKED)
 UPDATE answer_match_jobs j SET state='leased',lease=$1,lease_until=now()+interval '45 seconds',attempts=attempts+1
 FROM candidate c WHERE j.scope=c.scope AND j.request_id=c.request_id RETURNING j.scope,j.request_id,j.version`, lease).Scan(&scope, &id, &version)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	finish := func(decision AutomaticMatch, reason string) error {
		return s.tx(ctx, Actor{Scope: scope}, func(tx pgx.Tx) error {
			var active bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM answer_match_jobs j JOIN scopes s ON s.id=j.scope WHERE j.scope=$1 AND j.request_id=$2 AND j.lease=$3 AND j.state='leased' AND j.lease_until>now() AND s.automatic_answers AND s.assistance_version=$4)`, scope, id, lease, version).Scan(&active); err != nil {
				return err
			}
			if !active {
				return nil
			}
			live, err := s.automaticRequesterCurrent(ctx, tx, scope, id)
			if err != nil {
				return err
			}
			if live && decision.Equivalent && decision.Confidence >= 0.98 && decision.Confidence <= 1 && ValidText(decision.Reason, 1000) {
				// Re-read every boundary after the model call, inside the decision transaction.
				r, err := scanRequest(tx.QueryRow(ctx, `SELECT `+requestFields+` FROM requests r WHERE r.scope=$1 AND r.id=$2`, scope, id))
				if err != nil {
					return err
				}
				answer, err := scanAnswer(tx.QueryRow(ctx, `SELECT `+answerFields+` FROM shared_answers WHERE scope=$1 AND id=$2 AND NOT invalidated AND valid_until>now()`, scope, decision.AnswerID))
				if err == nil && r.State == "open" && r.Kind == "question" && r.SharedKey != "" && answer.Key == r.SharedKey && contains(answer.Audience, r.RequesterID) && s.publisherCurrent(ctx, tx, scope, answer) {
					var sourceChoices []byte
					if err = tx.QueryRow(ctx, `SELECT choices FROM requests WHERE scope=$1 AND id=$2`, scope, answer.SourceRequest).Scan(&sourceChoices); err != nil {
						return err
					}
					var choices []string
					_ = json.Unmarshal(sourceChoices, &choices)
					author, err := principal(ctx, tx, scope, answer.AuthorID)
					if err == nil && !author.Disabled && (len(choices) == 0 && len(r.Choices) == 0 || string(JSON(choices)) == string(JSON(r.Choices))) {
						actor := Actor{Principal: author, Scope: scope}
						_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, scope, actor.ID).Scan(&actor.Admin)
						// A denied automatic response must not abort bookkeeping or poison the transaction.
						nested, err := tx.Begin(ctx)
						if err != nil {
							return err
						}
						var resolved Request
						err = s.respondTx(ctx, nested, actor, r.ID, Response{Outcome: "answered", Answer: answer.Answer, Version: r.Version, SourceAnswerID: answer.ID, ReuseMethod: "automatic", ReuseReason: decision.Reason}, &resolved)
						if err == nil {
							err = nested.Commit(ctx)
						} else {
							_ = nested.Rollback(ctx)
						}
						if err == nil {
							if err = s.requestEvent(ctx, tx, actor, resolved, "request.answer_reused"); err != nil {
								return err
							}
							reason = "resolved"
							if _, err = tx.Exec(ctx, `UPDATE deliveries SET state='cancelled',last_error='Answered from a shared answer' WHERE scope=$1 AND operation_id=$2 AND state IN ('queued','waiting')`, scope, id); err != nil {
								return err
							}
						} else if ErrorStatus(err) >= 500 {
							return err
						} else {
							reason = "current permissions or request state changed"
						}
					}
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE answer_match_jobs SET state='done' WHERE scope=$1 AND request_id=$2 AND lease=$3`, scope, id, lease); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE deliveries SET next_at=now() WHERE scope=$1 AND operation_id=$2 AND state='queued'`, scope, id)
			if err != nil {
				return err
			}
			var requesterID string
			if err = tx.QueryRow(ctx, `SELECT requester_id FROM requests WHERE scope=$1 AND id=$2`, scope, id).Scan(&requesterID); err != nil {
				return err
			}
			return event(ctx, tx, Actor{Scope: scope}, "answer.automatic_checked", "", requesterID, map[string]string{"request_id": id, "outcome": reason})
		})
	}
	r, err := scanRequest(s.Pool.QueryRow(ctx, `SELECT `+requestFields+` FROM requests r WHERE r.scope=$1 AND r.id=$2`, scope, id))
	if err != nil {
		return true, err
	}
	if r.State != "open" {
		return true, finish(AutomaticMatch{}, "question already ended")
	}
	live, err := s.automaticRequesterCurrent(ctx, s.Pool, scope, id)
	if err != nil {
		return true, err
	}
	if !live {
		return true, finish(AutomaticMatch{}, "requester authority changed")
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+answerFields+`,(SELECT source.body FROM requests source WHERE source.scope=a.scope AND source.id=a.source_request) FROM shared_answers a WHERE a.scope=$1 AND a.key=$2 AND a.audience ? $3 AND NOT a.invalidated AND a.valid_until>now() AND EXISTS(SELECT 1 FROM requests source WHERE source.scope=a.scope AND source.id=a.source_request AND (source.choices=$4::jsonb OR (source.choices IN ('null'::jsonb,'[]'::jsonb) AND $4::jsonb IN ('null'::jsonb,'[]'::jsonb)))) ORDER BY a.created_at DESC LIMIT 8`, scope, r.SharedKey, r.RequesterID, JSON(r.Choices))
	if err != nil {
		return true, err
	}
	candidates := []SharedAnswer{}
	for rows.Next() {
		var sourceBody string
		answer, err := scanAnswer(rows, &sourceBody)
		if err != nil {
			rows.Close()
			return true, err
		}
		if sourceBody != "" {
			answer.Question += "\nContext: " + sourceBody
		}
		candidates = append(candidates, answer)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return true, err
	}
	safe := candidates[:0]
	for _, answer := range candidates {
		if s.publisherCurrent(ctx, s.Pool, scope, answer) {
			safe = append(safe, answer)
		}
	}
	if len(safe) == 0 {
		return true, finish(AutomaticMatch{}, "no eligible published answer")
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	decision, err := s.AutomaticMatcher(bounded, r, safe)
	if err != nil {
		return true, finish(AutomaticMatch{}, "matching unavailable; question remains open")
	}
	found := false
	for _, answer := range safe {
		if answer.ID == decision.AnswerID {
			found = true
		}
	}
	if !found {
		decision = AutomaticMatch{}
	}
	return true, finish(decision, "no sufficiently certain match")
}

func (s *Store) automaticRequesterCurrent(ctx context.Context, db DB, scope, id string) (bool, error) {
	var live bool
	err := db.QueryRow(ctx, `SELECT NOT p.disabled AND (j.credential_id='' OR EXISTS(SELECT 1 FROM credentials c WHERE c.scope=j.scope AND c.id=j.credential_id AND c.principal_id=p.id AND NOT c.revoked AND (c.expires_at IS NULL OR c.expires_at>now()))) FROM answer_match_jobs j JOIN requests r ON r.scope=j.scope AND r.id=j.request_id JOIN principals p ON p.scope=r.scope AND p.id=r.requester_id WHERE j.scope=$1 AND j.request_id=$2`, scope, id).Scan(&live)
	return live, normalize(err)
}
