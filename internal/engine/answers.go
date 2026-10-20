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
type MatchSuggestion struct {
	AnswerID string `json:"answer_id"`
	Reason   string `json:"reason"`
}
type Proposal struct {
	ID        string       `json:"id"`
	RequestID string       `json:"request_id"`
	Answer    SharedAnswer `json:"answer"`
	Reason    string       `json:"reason"`
	State     string       `json:"state"`
}
type Matcher func(context.Context, Request, []SharedAnswer) ([]MatchSuggestion, error)

func contextHash(key, title, body string, choices []string) string {
	return Digest(string(JSON([]any{key, title, body, choices})))
}

const answerFields = `id,key,context_hash,question,answer,author_id,publisher_id,audience,valid_until,invalidated,source_request`

func scanAnswer(row pgx.Row) (SharedAnswer, error) {
	var a SharedAnswer
	var value, audience []byte
	e := row.Scan(&a.ID, &a.Key, &a.ContextHash, &a.Question, &value, &a.AuthorID, &a.PublisherID, &audience, &a.ValidUntil, &a.Invalidated, &a.SourceRequest)
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
	return a.Admin || s.allowed(ctx, db, a, "manage", Target{"answer", "*"}, "", "")
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
	rows, e := s.Pool.Query(ctx, `SELECT `+answerFields+` FROM shared_answers WHERE scope=$1 AND NOT invalidated AND valid_until>now() AND ($2 OR publisher_id=$3 OR audience ? $3) ORDER BY created_at DESC LIMIT 100`, a.Scope, a.Admin, a.ID)
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
		if s.publisherCurrent(ctx, s.Pool, a.Scope, v) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *Store) PublishAnswer(ctx context.Context, a Actor, in PublishInput) (SharedAnswer, error) {
	var out SharedAnswer
	if len(in.Audience) == 0 || len(in.Audience) > 1000 {
		return out, Bad("An explicit reuse audience of 1 to 1000 identities is required.")
	}
	if in.ValidUntil.IsZero() {
		in.ValidUntil = time.Now().Add(24 * time.Hour)
	}
	if in.ValidUntil.Before(time.Now()) || in.ValidUntil.After(time.Now().Add(90*24*time.Hour)) {
		return out, Bad("Answer expiry must be within 90 days.")
	}
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
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
		out = SharedAnswer{ID: ID("ans"), Key: key, ContextHash: contextHash(key, r.Title, r.Body, r.Choices), Question: r.Title, Answer: r.Response.Answer, AuthorID: *r.ResolvedBy, PublisherID: a.ID, Audience: in.Audience, ValidUntil: in.ValidUntil, SourceRequest: r.ID}
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
				if e = s.resolveFromAnswer(ctx, tx, a, r, out); e != nil {
					return e
				}
			}
		}
		return nil
	})
	return out, e
}
func (s *Store) resolveFromAnswer(ctx context.Context, db DB, a Actor, r Request, answer SharedAnswer) error {
	response := Response{Outcome: "answered", Answer: answer.Answer, Version: r.Version, SourceAnswerID: answer.ID}
	if _, e := db.Exec(ctx, `UPDATE requests SET state='resolved',response=$3,resolved_by=$4,resolved_at=now() WHERE scope=$1 AND id=$2 AND state='open'`, a.Scope, r.ID, JSON(response), answer.AuthorID); e != nil {
		return e
	}
	r.State = "resolved"
	r.Response = &response
	r.ResolvedBy = &answer.AuthorID
	now := time.Now()
	r.ResolvedAt = &now
	if e := s.requestEvent(ctx, db, a, r, "request.answer_reused"); e != nil {
		return e
	}
	_, e := db.Exec(ctx, `INSERT INTO deliveries(id,scope,operation_id,recipient_id,payload,expires_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, ID("del"), a.Scope, r.ID+"/shared-result", r.RequesterID, JSON(map[string]any{"type": "request.resolved", "request": r, "conversation_id": r.ConversationID, "_policy": map[string]string{"engine_result": r.ID}}), time.Now().Add(30*24*time.Hour))
	return e
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
			if e = s.resolveFromAnswer(ctx, db, a, *r, answer); e != nil {
				return e
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
		if !s.canPublish(ctx, tx, a) {
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
func (s *Store) SuggestAnswers(ctx context.Context, a Actor, id string) ([]Proposal, error) {
	r, e := s.Request(ctx, a, id)
	if e != nil {
		return nil, e
	}
	if r.Kind != "question" || r.State != "open" {
		return nil, Bad("Only open informational questions may receive suggestions.")
	}
	answers, e := s.Answers(ctx, a)
	if e != nil {
		return nil, e
	}
	candidates := []SharedAnswer{}
	for _, v := range answers {
		if contains(v.Audience, r.RequesterID) {
			candidates = append(candidates, v)
			if len(candidates) >= 8 {
				break
			}
		}
	}
	if s.Matcher == nil || len(candidates) == 0 {
		return []Proposal{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	suggestions, e := s.Matcher(ctx, r, candidates)
	if e != nil {
		return []Proposal{}, nil
	}
	out := []Proposal{}
	e = s.tx(ctx, a, func(tx pgx.Tx) error {
		for _, v := range suggestions {
			if len(out) >= 5 {
				break
			}
			for _, candidate := range candidates {
				if candidate.ID != v.AnswerID || !ValidText(v.Reason, 1000) {
					continue
				}
				p := Proposal{ID: ID("match"), RequestID: id, Answer: candidate, Reason: v.Reason, State: "proposed"}
				if _, e := tx.Exec(ctx, `INSERT INTO answer_proposals(id,scope,request_id,answer_id,reviewer_id,reason) VALUES($1,$2,$3,$4,$5,$6)`, p.ID, a.Scope, id, candidate.ID, a.ID, v.Reason); e != nil {
					return e
				}
				out = append(out, p)
			}
		}
		return nil
	})
	return out, e
}
func (s *Store) DecideProposal(ctx context.Context, a Actor, id string, accept bool) (Request, error) {
	var r Request
	var answerID, requestID, reviewer, state string
	e := s.Pool.QueryRow(ctx, `SELECT request_id,answer_id,reviewer_id,state FROM answer_proposals WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&requestID, &answerID, &reviewer, &state)
	if e != nil {
		return r, Missing()
	}
	if reviewer != a.ID || state != "proposed" {
		return r, Denied()
	}
	r, e = s.Request(ctx, a, requestID)
	if e != nil {
		return r, e
	}
	if !accept {
		_, e = s.Pool.Exec(ctx, `UPDATE answer_proposals SET state='rejected' WHERE scope=$1 AND id=$2`, a.Scope, id)
		return r, e
	}
	answers, e := s.Answers(ctx, a)
	if e != nil {
		return r, e
	}
	var selected *SharedAnswer
	for _, v := range answers {
		if v.ID == answerID && contains(v.Audience, r.RequesterID) {
			v := v
			selected = &v
		}
	}
	if selected == nil {
		return r, Denied()
	}
	if r.Kind != "question" {
		return r, Denied()
	}
	r, e = s.Respond(ctx, a, requestID, Response{Outcome: "answered", Answer: selected.Answer, Version: r.Version})
	if e != nil {
		return r, e
	}
	_, e = s.Pool.Exec(ctx, `UPDATE answer_proposals SET state='accepted' WHERE scope=$1 AND id=$2`, a.Scope, id)
	return r, e
}
