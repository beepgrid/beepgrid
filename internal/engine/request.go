package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

const requestFields = `r.id,r.requester_id,COALESCE(r.requester_instance,''),r.conversation_id,r.kind,r.title,r.body,r.choices,r.action,CASE WHEN r.state='open' AND r.expires_at<=now() THEN 'expired' ELSE r.state END,r.version,r.response,r.resolved_by,r.resolved_at,r.expires_at,r.created_at,r.shared_key`

func scanRequest(row pgx.Row, extra ...any) (Request, error) {
	var r Request
	var choices, action, response []byte
	targets := []any{&r.ID, &r.RequesterID, &r.RequesterInstanceID, &r.ConversationID, &r.Kind, &r.Title, &r.Body, &choices, &action, &r.State, &r.Version, &response, &r.ResolvedBy, &r.ResolvedAt, &r.ExpiresAt, &r.CreatedAt, &r.SharedKey}
	e := row.Scan(append(targets, extra...)...)
	_ = json.Unmarshal(choices, &r.Choices)
	if len(action) > 0 && string(action) != "null" {
		_ = json.Unmarshal(action, &r.Action)
	}
	if len(response) > 0 {
		_ = json.Unmarshal(response, &r.Response)
	}
	return r, normalize(e)
}
func (s *Store) request(ctx context.Context, db DB, a Actor, id string) (Request, error) {
	r, e := scanRequest(db.QueryRow(ctx, `SELECT `+requestFields+` FROM requests r WHERE r.scope=$1 AND r.id=$2 AND ($3 OR r.requester_id=$4 OR EXISTS(SELECT 1 FROM request_audience ra WHERE ra.scope=r.scope AND ra.request_id=r.id AND ra.principal_id=$4))`, a.Scope, id, a.Admin, a.ID))
	if e != nil {
		return r, e
	}
	if a.ID != r.RequesterID && !a.Admin {
		requester, err := principal(ctx, db, a.Scope, r.RequesterID)
		if err != nil {
			return r, err
		}
		if !s.allowed(ctx, db, a, "view", Target{requester.Kind, requester.ID}, "", "") {
			return r, Missing()
		}
		_ = db.QueryRow(ctx, `SELECT conversation_id FROM request_audience WHERE scope=$1 AND request_id=$2 AND principal_id=$3`, a.Scope, id, a.ID).Scan(&r.ConversationID)
	}
	if r.Response != nil && r.Response.SourceAnswerID != "" {
		answer, e := scanAnswer(db.QueryRow(ctx, `SELECT `+answerFields+` FROM shared_answers WHERE scope=$1 AND id=$2`, a.Scope, r.Response.SourceAnswerID))
		r.Response.SourceInvalidated = e != nil || answer.Invalidated || answer.ValidUntil.Before(time.Now()) || !s.publisherCurrent(ctx, db, a.Scope, answer)
	}
	return r, nil
}
func (s *Store) Request(ctx context.Context, a Actor, id string) (Request, error) {
	return s.request(ctx, s.Pool, a, id)
}
func (s *Store) Requests(ctx context.Context, a Actor) ([]Request, error) {
	ctx = withPolicySnapshot(ctx)
	fields := strings.Replace(requestFields, "r.conversation_id", "CASE WHEN $2 OR r.requester_id=$3 THEN r.conversation_id ELSE ra.conversation_id END", 1)
	rows, e := s.Pool.Query(ctx, `SELECT `+fields+`,p.kind FROM requests r JOIN principals p ON p.id=r.requester_id AND p.scope=r.scope LEFT JOIN request_audience ra ON ra.scope=r.scope AND ra.request_id=r.id AND ra.principal_id=$3 WHERE r.scope=$1 AND ($2 OR r.requester_id=$3 OR ra.principal_id IS NOT NULL) ORDER BY r.created_at DESC,r.id DESC LIMIT 200`, a.Scope, a.Admin, a.ID)
	if e != nil {
		return nil, e
	}
	type item struct {
		request Request
		kind    string
	}
	all := []item{}
	for rows.Next() {
		var kind string
		r, e := scanRequest(rows, &kind)
		if e != nil {
			rows.Close()
			return nil, e
		}
		all = append(all, item{r, kind})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []Request{}
	for _, x := range all {
		r := x.request
		if !a.Admin && a.ID != r.RequesterID && !s.allowed(ctx, s.Pool, a, "view", Target{x.kind, r.RequesterID}, "", "") {
			continue
		}
		if r.Response != nil && r.Response.SourceAnswerID != "" {
			answer, e := scanAnswer(s.Pool.QueryRow(ctx, `SELECT `+answerFields+` FROM shared_answers WHERE scope=$1 AND id=$2`, a.Scope, r.Response.SourceAnswerID))
			r.Response.SourceInvalidated = e != nil || answer.Invalidated || answer.ValidUntil.Before(time.Now()) || !s.publisherCurrent(ctx, s.Pool, a.Scope, answer)
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Store) Ask(ctx context.Context, a Actor, in AskInput, key string) (Request, error) {
	var out Request
	stable := in
	if in.Kind == "" {
		in.Kind = "question"
	}
	if in.Kind != "question" && in.Kind != "approval" && in.Kind != "task" {
		return out, Bad("kind must be question, approval or task.")
	}
	if !ValidText(in.Title, 200) || len(in.Body) > 32768 || len(in.Choices) > 20 {
		return out, Bad("A title and bounded body/choices are required.")
	}
	if in.ExpiresAt.IsZero() {
		in.ExpiresAt = time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Second)
	}
	if in.ExpiresAt.Before(time.Now()) || in.ExpiresAt.After(time.Now().Add(90*24*time.Hour)) {
		return out, Bad("Expiry must be within 90 days.")
	}
	if in.Kind == "approval" && (in.Action == nil || !ValidText(in.Action.Name, 120) || in.Action.ExecutorID == "" || in.Action.Resource == "" || in.Action.ResourceVersion == "" || in.Action.OperationKey == "") {
		return out, Bad("An approval requires an exact action, executor, resource version and operation key.")
	}
	if in.Kind != "approval" && in.Action != nil {
		return out, Bad("Only approvals may carry an execution descriptor.")
	}
	if len(in.SharedKey) > 200 {
		return out, Bad("Shared key exceeds 200 characters.")
	}
	if in.Kind != "question" && in.SharedKey != "" {
		return out, Bad("Only informational questions may use shared keys.")
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		return idempotent(ctx, tx, a, "ask", key, stable, &out, func() error {
			var rs []recipient
			var e error
			if in.ConversationID != "" {
				rs, e = s.conversationRecipients(ctx, tx, a, in.ConversationID, "ask", in.Action)
			} else {
				rs, e = s.resolve(ctx, tx, a, in.Target, "ask", "", in.Action)
			}
			if e != nil {
				return e
			}
			if in.Action != nil {
				executor, e := principal(ctx, tx, a.Scope, in.Action.ExecutorID)
				if e != nil || executor.Disabled {
					return Missing()
				}
				if !s.allowed(ctx, tx, a, "ask", Target{executor.Kind, executor.ID}, "*", in.Action.Name, in.Action) {
					return Denied()
				}
			}
			conv := in.ConversationID
			if conv == "" {
				if len(rs) == 1 {
					conv, e = newConversation(ctx, tx, a, in.Title, rs[0].ID)
				} else {
					conv, e = newConversation(ctx, tx, a, in.Title)
				}
				if e != nil {
					return e
				}
			}
			out = Request{RequesterInstanceID: a.InstanceID, ID: ID("req"), RequesterID: a.ID, ConversationID: conv, Kind: in.Kind, Title: in.Title, Body: in.Body, Choices: in.Choices, Action: in.Action, State: "open", Version: 1, ExpiresAt: in.ExpiresAt, CreatedAt: time.Now(), SharedKey: in.SharedKey}
			if _, e = tx.Exec(ctx, `INSERT INTO requests(id,scope,requester_id,requester_instance,conversation_id,kind,title,body,choices,action,expires_at,shared_key) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,$10,$11,$12)`, out.ID, a.Scope, a.ID, a.InstanceID, conv, in.Kind, in.Title, in.Body, JSON(in.Choices), JSON(in.Action), in.ExpiresAt, in.SharedKey); e != nil {
				return e
			}
			for _, r := range rs {
				projection := conv
				if in.ConversationID == "" && len(rs) > 1 {
					projection, e = newConversation(ctx, tx, a, in.Title, r.ID)
					if e != nil {
						return e
					}
				}
				if _, e = tx.Exec(ctx, `INSERT INTO request_audience(scope,request_id,principal_id,conversation_id,policy_target) VALUES($1,$2,$3,$4,$5)`, a.Scope, out.ID, r.ID, projection, JSON(r.PolicyTarget)); e != nil {
					return e
				}
				view := out
				view.ConversationID = projection
				if e = event(ctx, tx, a, "request.created", projection, r.ID, view); e != nil {
					return e
				}
				if e = s.enqueue(ctx, tx, a, r, out.ID, projection, "ask", map[string]any{"type": "request", "request": view}, in.ExpiresAt); e != nil {
					return e
				}
			}
			if e = event(ctx, tx, a, "request.created", conv, a.ID, out); e != nil {
				return e
			}
			return s.reuseExact(ctx, tx, a, &out)
		})
	})
	if err == nil {
		return s.Request(ctx, a, out.ID)
	}
	return out, err
}
func (s *Store) Respond(ctx context.Context, a Actor, id string, in Response) (Request, error) {
	var out Request
	if in.SourceAnswerID != "" || in.SourceInvalidated || in.ResponderInstanceID != "" {
		return out, Bad("Answer provenance is assigned by the server.")
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error { return s.respondTx(ctx, tx, a, id, in, &out) })
	return out, err
}
func (s *Store) respondTx(ctx context.Context, tx pgx.Tx, a Actor, id string, in Response, out *Request) error {
	in.ResponderInstanceID = a.InstanceID
	r, e := s.request(ctx, tx, a, id)
	if e != nil {
		return e
	}
	if in.Version != r.Version {
		return Conflict("The request version changed.")
	}
	if r.State != "open" {
		if r.State == "resolved" && r.ResolvedBy != nil && *r.ResolvedBy == a.ID && r.Response != nil && string(JSON(*r.Response)) == string(JSON(in)) {
			*out = r
			return nil
		}
		return Conflict("The request is already " + r.State + ".")
	}
	var eligible bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM request_audience WHERE scope=$1 AND request_id=$2 AND principal_id=$3)`, a.Scope, id, a.ID).Scan(&eligible); e != nil {
		return e
	}
	if eligible && !a.Admin {
		var target Target
		var raw []byte
		if e = tx.QueryRow(ctx, `SELECT policy_target FROM request_audience WHERE scope=$1 AND request_id=$2 AND principal_id=$3`, a.Scope, id, a.ID).Scan(&raw); e != nil {
			return e
		}
		_ = json.Unmarshal(raw, &target)
		if target.Kind == "group" {
			var member bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE scope=$1 AND group_id=$2 AND principal_id=$3)`, a.Scope, target.ID, a.ID).Scan(&member)
			if !member {
				return Denied()
			}
		}
	}
	if !eligible && !a.Admin {
		return Denied()
	}
	requester, e := principal(ctx, tx, a.Scope, r.RequesterID)
	if e != nil {
		return e
	}
	actionName := ""
	if r.Action != nil {
		actionName = r.Action.Name
	}
	permission := "message"
	if r.Kind == "approval" {
		permission = "decide"
	}
	var originalTarget Target
	var rawTarget []byte
	if e = tx.QueryRow(ctx, `SELECT policy_target FROM request_audience WHERE scope=$1 AND request_id=$2 AND principal_id=$3`, a.Scope, id, a.ID).Scan(&rawTarget); e == nil {
		_ = json.Unmarshal(rawTarget, &originalTarget)
	} else if !a.Admin {
		return Denied()
	}
	if originalTarget.Kind != "" {
		proposer := Actor{Principal: requester, Scope: a.Scope}
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, a.Scope, requester.ID).Scan(&proposer.Admin)
		if requester.Disabled || !s.allowed(ctx, tx, proposer, "ask", originalTarget, "*", actionName, r.Action) {
			return Denied()
		}
	}
	if !s.allowed(ctx, tx, a, permission, Target{requester.Kind, requester.ID}, a.ProviderConnection, actionName, r.Action) {
		return Denied()
	}
	switch r.Kind {
	case "approval":
		if in.Outcome != "approved" && in.Outcome != "denied" {
			return Bad("Approval outcome must be approved or denied.")
		}
	case "question":
		if in.Outcome != "answered" {
			return Bad("Question outcome must be answered.")
		}
		if in.Answer == nil {
			return Bad("An answer is required.")
		}
		if len(r.Choices) > 0 {
			match := false
			for _, c := range r.Choices {
				if c == in.Answer {
					match = true
				}
			}
			if !match {
				return Bad("Answer must match a declared choice.")
			}
		}
	case "task":
		if in.Outcome != "completed" && in.Outcome != "failed" && in.Outcome != "rejected" {
			return Bad("Task outcome must be completed, failed or rejected.")
		}
	}
	if len(JSON(in)) > 32768 {
		return Bad("Response too large.")
	}
	if _, e = tx.Exec(ctx, `UPDATE requests SET state='resolved',response=$3,resolved_by=$4,resolved_at=now(),decision_connection=$5 WHERE scope=$1 AND id=$2 AND state='open'`, a.Scope, id, JSON(in), a.ID, a.ProviderConnection); e != nil {
		return e
	}
	*out, e = s.request(ctx, tx, a, id)
	if e != nil {
		return e
	}
	if e = s.requestEvent(ctx, tx, a, *out, "request.resolved"); e != nil {
		return e
	}
	// The canonical result has already passed response authorization. Notify the original requester.
	_, e = tx.Exec(ctx, `INSERT INTO deliveries(id,scope,operation_id,recipient_id,payload,expires_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, ID("del"), a.Scope, id+"/result", requester.ID, JSON(map[string]any{"type": "request.resolved", "request": out, "conversation_id": out.ConversationID, "_policy": map[string]string{"engine_result": id}}), time.Now().Add(30*24*time.Hour))
	return e
}
func (s *Store) requestEvent(ctx context.Context, db DB, a Actor, r Request, kind string) error {
	if e := event(ctx, db, a, kind, r.ConversationID, r.RequesterID, r); e != nil {
		return e
	}
	rows, e := db.Query(ctx, `SELECT principal_id,conversation_id FROM request_audience WHERE scope=$1 AND request_id=$2`, a.Scope, r.ID)
	if e != nil {
		return e
	}
	type view struct{ pid, conv string }
	all := []view{}
	for rows.Next() {
		var v view
		if e = rows.Scan(&v.pid, &v.conv); e != nil {
			rows.Close()
			return e
		}
		all = append(all, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range all {
		copy := r
		copy.ConversationID = v.conv
		if e = event(ctx, db, a, kind, v.conv, v.pid, copy); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) EndRequest(ctx context.Context, a Actor, id, state string, version int) (Request, error) {
	var out Request
	if state != "cancelled" && state != "superseded" {
		return out, Bad("Invalid terminal state.")
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		r, e := s.request(ctx, tx, a, id)
		if e != nil {
			return e
		}
		if a.ID != r.RequesterID && !a.Admin {
			return Denied()
		}
		if r.Version != version || r.State != "open" {
			return Conflict("Request is no longer open at that version.")
		}
		if _, e = tx.Exec(ctx, `UPDATE requests SET state=$3,resolved_at=now() WHERE scope=$1 AND id=$2`, a.Scope, id, state); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE deliveries SET state='cancelled' WHERE scope=$1 AND operation_id=$2 AND state IN ('queued','leased','waiting')`, a.Scope, id); e != nil {
			return e
		}
		out, e = s.request(ctx, tx, a, id)
		if e != nil {
			return e
		}
		return s.requestEvent(ctx, tx, a, out, "request."+state)
	})
	return out, err
}
func (s *Store) Redeem(ctx context.Context, a Actor, id string, action Action) (Execution, error) {
	var out Execution
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		r, e := scanRequest(tx.QueryRow(ctx, `SELECT `+requestFields+` FROM requests r WHERE r.scope=$1 AND r.id=$2`, a.Scope, id))
		if e != nil {
			return e
		}
		if r.Kind != "approval" || r.State != "resolved" || r.Response == nil || r.Response.Outcome != "approved" || r.Action == nil || r.ExpiresAt.Before(time.Now()) {
			return Conflict("No valid approval is available.")
		}
		if a.ID != r.Action.ExecutorID || string(JSON(action)) != string(JSON(*r.Action)) {
			return Denied()
		}
		requester, e := principal(ctx, tx, a.Scope, r.RequesterID)
		if e != nil || requester.Disabled {
			return Denied()
		}
		proposer := Actor{Principal: requester, Scope: a.Scope}
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, a.Scope, requester.ID).Scan(&proposer.Admin)
		if !s.allowed(ctx, tx, proposer, "ask", Target{a.Kind, a.ID}, "*", action.Name, &action) {
			return Denied()
		}
		if r.ResolvedBy == nil {
			return Denied()
		}
		approver, e := principal(ctx, tx, a.Scope, *r.ResolvedBy)
		if e != nil || approver.Disabled {
			return Denied()
		}
		decider := Actor{Principal: approver, Scope: a.Scope}
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, a.Scope, approver.ID).Scan(&decider.Admin)
		var decisionConnection string
		_ = tx.QueryRow(ctx, `SELECT decision_connection FROM requests WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&decisionConnection)
		if !s.allowed(ctx, tx, decider, "decide", Target{requester.Kind, requester.ID}, decisionConnection, action.Name, &action) {
			return Denied()
		}
		// The approved audience and the proposer's contact grant must still be valid at execution.
		var originalTarget Target
		var targetJSON []byte
		err := tx.QueryRow(ctx, `SELECT policy_target FROM request_audience WHERE scope=$1 AND request_id=$2 AND principal_id=$3`, a.Scope, id, *r.ResolvedBy).Scan(&targetJSON)
		if err == nil {
			if json.Unmarshal(targetJSON, &originalTarget) != nil {
				return Denied()
			}
			if !s.allowed(ctx, tx, proposer, "ask", originalTarget, "*", action.Name, &action) {
				return Denied()
			}
			if originalTarget.Kind == "group" {
				var member bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE scope=$1 AND group_id=$2 AND principal_id=$3)`, a.Scope, originalTarget.ID, *r.ResolvedBy).Scan(&member); err != nil || !member {
					return Denied()
				}
			}
		} else if err != pgx.ErrNoRows || !decider.Admin {
			return Denied()
		}
		var raw, result []byte
		e = tx.QueryRow(ctx, `SELECT id,request_id,executor_id,operation_key,descriptor,state,result FROM execution_intents WHERE scope=$1 AND request_id=$2`, a.Scope, id).Scan(&out.ID, &out.RequestID, &out.ExecutorID, &out.OperationKey, &raw, &out.State, &result)
		if e == nil {
			_ = json.Unmarshal(raw, &out.Action)
			_ = json.Unmarshal(result, &out.Result)
			return nil
		}
		if e != pgx.ErrNoRows {
			return e
		}
		out = Execution{ID: ID("exe"), RequestID: id, ExecutorID: a.ID, OperationKey: action.OperationKey, Action: action, State: "authorized"}
		if _, e = tx.Exec(ctx, `INSERT INTO execution_intents(id,scope,request_id,executor_id,operation_key,descriptor) VALUES($1,$2,$3,$4,$5,$6)`, out.ID, a.Scope, id, a.ID, action.OperationKey, JSON(action)); e != nil {
			return e
		}
		return event(ctx, tx, a, "execution.authorized", r.ConversationID, a.ID, out)
	})
	return out, err
}
func (s *Store) ReportExecution(ctx context.Context, a Actor, id, state string, result any) (Execution, error) {
	var out Execution
	if state != "running" && state != "succeeded" && state != "failed" && state != "unknown" {
		return out, Bad("Invalid reported execution state.")
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		var raw, previousResult []byte
		var prev string
		e := tx.QueryRow(ctx, `SELECT id,request_id,executor_id,operation_key,descriptor,state,result FROM execution_intents WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&out.ID, &out.RequestID, &out.ExecutorID, &out.OperationKey, &raw, &prev, &previousResult)
		if e != nil {
			return e
		}
		if out.ExecutorID != a.ID {
			return Denied()
		}
		if prev == "succeeded" || prev == "failed" {
			var saved any
			_ = json.Unmarshal(previousResult, &saved)
			if prev != state || string(JSON(saved)) != string(JSON(result)) {
				return Conflict("Execution already has a different terminal report.")
			}
			_ = json.Unmarshal(raw, &out.Action)
			_ = json.Unmarshal(previousResult, &out.Result)
			out.State = prev
			return nil
		}
		if len(JSON(result)) > 32768 {
			return Bad("Result too large.")
		}
		_ = json.Unmarshal(raw, &out.Action)
		out.State = state
		out.Result = result
		if _, e = tx.Exec(ctx, `UPDATE execution_intents SET state=$3,result=$4,updated_at=now() WHERE scope=$1 AND id=$2`, a.Scope, id, state, JSON(result)); e != nil {
			return e
		}
		return event(ctx, tx, a, "execution.reported", "", a.ID, map[string]any{"execution": out, "evidence": "worker_reported", "instance_id": a.InstanceID})
	})
	return out, err
}
func (s *Store) Executions(ctx context.Context, a Actor) ([]Execution, error) {
	rows, e := s.Pool.Query(ctx, `SELECT id,request_id,executor_id,operation_key,descriptor,state,result FROM execution_intents WHERE scope=$1 AND ($2 OR executor_id=$3 OR request_id IN(SELECT request_id FROM request_audience WHERE scope=$1 AND principal_id=$3) OR request_id IN(SELECT id FROM requests WHERE scope=$1 AND requester_id=$3)) ORDER BY updated_at DESC LIMIT 200`, a.Scope, a.Admin, a.ID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Execution{}
	for rows.Next() {
		var x Execution
		var descriptor, result []byte
		if e = rows.Scan(&x.ID, &x.RequestID, &x.ExecutorID, &x.OperationKey, &descriptor, &x.State, &result); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(descriptor, &x.Action)
		_ = json.Unmarshal(result, &x.Result)
		out = append(out, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	visible := []Execution{}
	for _, x := range out {
		if a.Admin || x.ExecutorID == a.ID {
			visible = append(visible, x)
			continue
		}
		if _, e := s.request(ctx, s.Pool, a, x.RequestID); e == nil {
			visible = append(visible, x)
		}
	}
	return visible, nil
}
