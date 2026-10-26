package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

type SharedAnswer struct {
	ID            string    `json:"id"`
	Key           string    `json:"key"`
	ContextHash   string    `json:"context_hash"`
	Question      string    `json:"question"`
	Answer        any       `json:"answer"`
	AuthorID      string    `json:"author_id"`
	PublisherID   string    `json:"publisher_id"`
	Audience      []string  `json:"audience"`
	ValidUntil    time.Time `json:"valid_until"`
	Invalidated   bool      `json:"invalidated"`
	SourceRequest string    `json:"source_request"`
}
type PublishInput struct {
	RequestID  string    `json:"request_id"`
	Key        string    `json:"key"`
	Audience   []string  `json:"audience"`
	ValidUntil time.Time `json:"valid_until"`
}

func contextHash(key, title, body string, choices []string) string {
	return Digest(string(JSON([]any{key, title, body, choices})))
}

const answerFields = `id,key,context_hash,question,answer,author_id,publisher_id,audience,valid_until,invalidated,source_request`

func scanAnswer(row pgx.Row, extra ...any) (SharedAnswer, error) {
	var a SharedAnswer
	var value, audience []byte
	targets := []any{&a.ID, &a.Key, &a.ContextHash, &a.Question, &value, &a.AuthorID, &a.PublisherID, &audience, &a.ValidUntil, &a.Invalidated, &a.SourceRequest}
	e := row.Scan(append(targets, extra...)...)
	_ = json.Unmarshal(value, &a.Answer)
	_ = json.Unmarshal(audience, &a.Audience)
	return a, normalize(e)
}
func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
func (s *Store) canPublish(ctx context.Context, db DB, a Actor) bool {
	return s.manages(ctx, db, a, "answer", a.ID)
}
func (s *Store) publisherCurrent(ctx context.Context, db DB, scope string, answer SharedAnswer) bool {
	p, e := principal(ctx, db, scope, answer.PublisherID)
	if e != nil || p.Disabled {
		return false
	}
	a := Actor{Principal: p, Scope: scope}
	_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, scope, p.ID).Scan(&a.Admin)
	return s.canPublish(ctx, db, a)
}
func (s *Store) Answers(ctx context.Context, a Actor) ([]SharedAnswer, error) {
	rules, err := roleRules(ctx, s.Pool, a)
	if err != nil {
		return nil, err
	}
	managesAnswers := a.Admin
	for _, r := range rules {
		if (r.Action == "manage" || r.Action == "*") && (r.TargetKind == "answer" || r.TargetKind == "*") {
			managesAnswers = true
		}
	}

	rows, e := s.Pool.Query(ctx, `SELECT `+answerFields+` FROM shared_answers WHERE scope=$1 AND NOT invalidated AND valid_until>now() AND ($2 OR publisher_id=$3 OR audience ? $3) ORDER BY created_at DESC LIMIT 100`, a.Scope, managesAnswers, a.ID)
	if e != nil {
		return nil, e
	}
	all := []SharedAnswer{}
	for rows.Next() {
		v, e := scanAnswer(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		all = append(all, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []SharedAnswer{}
	for _, v := range all {
		if (a.Admin || v.PublisherID == a.ID || contains(v.Audience, a.ID) || s.manages(ctx, s.Pool, a, "answer", v.ID)) && s.publisherCurrent(ctx, s.Pool, a.Scope, v) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *Store) PublishAnswer(ctx context.Context, a Actor, in PublishInput) (SharedAnswer, error) {
	var out SharedAnswer
	err := s.tx(ctx, a, func(tx pgx.Tx) error { return s.publishTx(ctx, tx, a, in, &out) })
	return out, err
}
func (s *Store) publishTx(ctx context.Context, tx pgx.Tx, a Actor, in PublishInput, out *SharedAnswer) error {
	if len(in.Audience) == 0 || len(in.Audience) > 1000 {
		return Bad("An explicit reuse audience of 1 to 1000 identities is required.")
	}
	if in.ValidUntil.IsZero() {
		in.ValidUntil = time.Now().Add(24 * time.Hour)
	}
	if in.ValidUntil.Before(time.Now()) || in.ValidUntil.After(time.Now().Add(90*24*time.Hour)) {
		return Bad("Answer expiry must be within 90 days.")
	}
	if !s.canPublish(ctx, tx, a) {
		return Denied()
	}
	r, e := s.request(ctx, tx, a, in.RequestID)
	if e != nil {
		return e
	}
	if r.Kind != "question" || r.State != "resolved" || r.Response == nil || r.Response.Outcome != "answered" || r.ResolvedBy == nil {
		return Bad("Only an answered informational question can be published.")
	}
	key := in.Key
	if key == "" {
		key = r.SharedKey
	}
	if !ValidText(key, 200) {
		return Bad("A scoped reuse key is required, for example project/policy/version.")
	}
	if r.SharedKey != "" && key != r.SharedKey {
		return Conflict("The source question already has a different shared key.")
	}
	for _, pid := range in.Audience {
		p, e := principal(ctx, tx, a.Scope, pid)
		if e != nil || p.Disabled {
			return Bad("Every reuse audience member must be active in this workspace.")
		}
	}
	*out = SharedAnswer{ID: ID("ans"), Key: key, ContextHash: contextHash(key, r.Title, r.Body, r.Choices), Question: r.Title, Answer: r.Response.Answer, AuthorID: *r.ResolvedBy, PublisherID: a.ID, Audience: in.Audience, ValidUntil: in.ValidUntil, SourceRequest: r.ID}
	if _, e = tx.Exec(ctx, `INSERT INTO shared_answers(id,scope,key,context_hash,question,answer,author_id,publisher_id,audience,valid_until,source_request) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, out.ID, a.Scope, key, out.ContextHash, out.Question, JSON(out.Answer), out.AuthorID, a.ID, JSON(out.Audience), in.ValidUntil, r.ID); e != nil {
		return e
	}
	if e = event(ctx, tx, a, "answer.published", "", a.ID, map[string]any{"id": out.ID, "source_request": r.ID, "audience_count": len(out.Audience)}); e != nil {
		return e
	}
	rows, e := tx.Query(ctx, `SELECT `+requestFields+` FROM requests r WHERE r.scope=$1 AND r.kind='question' AND r.state='open' AND r.expires_at>now() AND r.shared_key=$2`, a.Scope, key)
	if e != nil {
		return e
	}
	waiters := []Request{}
	for rows.Next() {
		r, e := scanRequest(rows)
		if e != nil {
			rows.Close()
			return e
		}
		waiters = append(waiters, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, r := range waiters {
		if contains(out.Audience, r.RequesterID) && contextHash(key, r.Title, r.Body, r.Choices) == out.ContextHash {
			if _, e = s.resolveFromAnswer(ctx, tx, a, r, *out); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Store) resolveFromAnswer(ctx context.Context, db DB, a Actor, r Request, answer SharedAnswer) (bool, error) {
	// Publication grants an explicit reuse audience, never another person's
	// response authority. Exact matching enforces current original group membership, contact rights
	// and responder eligibility.
	author, err := principal(ctx, db, a.Scope, answer.AuthorID)
	if err != nil {
		if ErrorStatus(err) == 404 {
			return false, nil
		}
		return false, err
	}
	if author.Disabled {
		return false, nil
	}
	responder := Actor{Principal: author, Scope: a.Scope}
	if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, a.Scope, author.ID).Scan(&responder.Admin); err != nil {
		return false, err
	}
	if _, err = s.request(ctx, db, responder, r.ID); err != nil {
		if ErrorStatus(err) < 500 {
			return false, nil
		}
		return false, err
	}
	if err = s.responseAuthorization(ctx, db, responder, r); err != nil {
		if ErrorStatus(err) < 500 {
			return false, nil
		}
		return false, err
	}
	response := Response{Outcome: "answered", Answer: answer.Answer, Version: r.Version, SourceAnswerID: answer.ID}
	if _, e := db.Exec(ctx, `UPDATE requests SET state='resolved',response=$3,resolved_by=$4,resolved_at=now() WHERE scope=$1 AND id=$2 AND state='open'`, a.Scope, r.ID, JSON(response), answer.AuthorID); e != nil {
		return false, e
	}
	r.State = "resolved"
	r.Response = &response
	r.ResolvedBy = &answer.AuthorID
	now := time.Now()
	r.ResolvedAt = &now
	if e := s.requestEvent(ctx, db, a, r, "request.answer_reused"); e != nil {
		return false, e
	}
	_, e := db.Exec(ctx, `INSERT INTO deliveries(id,scope,operation_id,recipient_id,payload,expires_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, ID("del"), a.Scope, r.ID+"/shared-result", r.RequesterID, JSON(map[string]any{"type": "request.resolved", "request": r, "conversation_id": r.ConversationID, "_policy": map[string]string{"engine_result": r.ID}}), time.Now().Add(30*24*time.Hour))
	return e == nil, e
}
func (s *Store) reuseExact(ctx context.Context, db DB, a Actor, r *Request) error {
	if r.Kind != "question" || r.SharedKey == "" {
		return nil
	}
	rows, e := db.Query(ctx, `SELECT `+answerFields+` FROM shared_answers WHERE scope=$1 AND key=$2 AND context_hash=$3 AND NOT invalidated AND valid_until>now() AND audience ? $4 ORDER BY created_at DESC LIMIT 5`, a.Scope, r.SharedKey, contextHash(r.SharedKey, r.Title, r.Body, r.Choices), a.ID)
	if e != nil {
		return e
	}
	answers := []SharedAnswer{}
	for rows.Next() {
		v, e := scanAnswer(rows)
		if e != nil {
			rows.Close()
			return e
		}
		answers = append(answers, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, answer := range answers {
		if s.publisherCurrent(ctx, db, a.Scope, answer) {
			reused, err := s.resolveFromAnswer(ctx, db, a, *r, answer)
			if err != nil {
				return err
			}
			if !reused {
				continue
			}
			r.State = "resolved"
			r.Response = &Response{Outcome: "answered", Answer: answer.Answer, Version: r.Version, SourceAnswerID: answer.ID}
			r.ResolvedBy = &answer.AuthorID
			return nil
		}
	}
	return nil
}
func (s *Store) InvalidateAnswer(ctx context.Context, a Actor, id string) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		var publisher string
		if e := tx.QueryRow(ctx, `SELECT publisher_id FROM shared_answers WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&publisher); e != nil {
			return normalize(e)
		}
		if !s.manages(ctx, tx, a, "answer", id) && !(publisher == a.ID && s.canPublish(ctx, tx, a)) {
			return Denied()
		}
		tag, e := tx.Exec(ctx, `UPDATE shared_answers SET invalidated=true WHERE scope=$1 AND id=$2`, a.Scope, id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return Missing()
		}
		if e = event(ctx, tx, a, "answer.invalidated", "", a.ID, map[string]string{"id": id}); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT `+requestFields+` FROM requests r WHERE r.scope=$1 AND r.response->>'source_answer_id'=$2`, a.Scope, id)
		if e != nil {
			return e
		}
		affected := []Request{}
		for rows.Next() {
			r, e := scanRequest(rows)
			if e != nil {
				rows.Close()
				return e
			}
			affected = append(affected, r)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, r := range affected {
			r.Response.SourceInvalidated = true
			if _, e = tx.Exec(ctx, `UPDATE requests SET response=$3 WHERE scope=$1 AND id=$2`, a.Scope, r.ID, JSON(r.Response)); e != nil {
				return e
			}
			if e = s.requestEvent(ctx, tx, a, r, "answer.invalidated"); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO deliveries(id,scope,operation_id,recipient_id,payload,expires_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, ID("del"), a.Scope, r.ID+"/invalidated/"+id, r.RequesterID, JSON(map[string]any{"type": "answer.invalidated", "request_id": r.ID, "answer_id": id, "_policy": map[string]string{"engine_result": r.ID}}), time.Now().Add(30*24*time.Hour)); e != nil {
				return e
			}
		}
		return nil
	})
}

func (s *Store) RespondAndShare(ctx context.Context, a Actor, id string, in Response, audience []string) (Request, error) {
	var out Request
	if in.SourceAnswerID != "" || in.SourceInvalidated || in.ResponderInstanceID != "" || in.ReuseMethod != "" || in.ReuseReason != "" {
		return out, Bad("Answer provenance is assigned by the server.")
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		return idempotent(ctx, tx, a, "answer-and-share", id, map[string]any{"response": in, "audience": audience}, &out, func() error {
			r, e := s.request(ctx, tx, a, id)
			if e != nil {
				return e
			}
			if r.Kind != "question" || r.SharedKey == "" {
				return Bad("Only informational questions with an explicit shared key may be answered together.")
			}
			if e = s.respondTx(ctx, tx, a, id, in, &out); e != nil {
				return e
			}
			var publication SharedAnswer
			return s.publishTx(ctx, tx, a, PublishInput{RequestID: id, Key: r.SharedKey, Audience: audience, ValidUntil: time.Now().Add(24 * time.Hour)}, &publication)
		})
	})
	return out, err
}
