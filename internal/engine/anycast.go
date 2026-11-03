package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// AnycastInput addresses one interchangeable member of an agent role. An empty
// kind sends a message; question, task and approval create ordinary requests.
type AnycastInput struct {
	RoleID        string    `json:"role_id"`
	Kind          string    `json:"kind,omitempty"`
	Body          string    `json:"body"`
	Title         string    `json:"title,omitempty"`
	Choices       []string  `json:"choices,omitempty"`
	Action        *Action   `json:"action,omitempty"`
	AttachmentIDs []string  `json:"attachment_ids,omitempty"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
}
type AnycastDispatch struct {
	ID             string       `json:"id"`
	RoleID         string       `json:"role_id"`
	RoleName       string       `json:"role_name"`
	RequesterID    string       `json:"requester_id"`
	State          string       `json:"state"`
	AgentID        string       `json:"agent_id,omitempty"`
	InstanceID     string       `json:"instance_id,omitempty"`
	ConversationID string       `json:"conversation_id,omitempty"`
	RequestID      string       `json:"request_id,omitempty"`
	Input          AnycastInput `json:"input"`
	CreatedAt      time.Time    `json:"created_at"`
	ExpiresAt      time.Time    `json:"expires_at"`
	LastError      string       `json:"last_error,omitempty"`
}

// Capacity is the number of concurrent Anycast jobs this runtime can accept,
// after accounting for unrelated work. Existing reservations are subtracted by
// the server. Refresh through the ordinary heartbeat, at most every 30 seconds.
type AnycastAvailability struct {
	Capacity int `json:"capacity"`
}
type anycastContextKey struct{}
type anycastSelection struct {
	id, role  string
	recipient recipient
}

const anycastFields = `d.id,d.role_id,COALESCE(g.name,'Agent role'),d.requester_id,
 CASE WHEN d.state='queued' AND d.expires_at<=now() THEN 'expired' ELSE d.state END,
 d.agent_id,d.instance_id,d.conversation_id,d.request_id,d.input,d.created_at,d.expires_at,d.last_error`

func scanAnycast(row pgx.Row) (AnycastDispatch, error) {
	var d AnycastDispatch
	e := row.Scan(&d.ID, &d.RoleID, &d.RoleName, &d.RequesterID, &d.State, &d.AgentID, &d.InstanceID, &d.ConversationID, &d.RequestID, jsonColumn{&d.Input}, &d.CreatedAt, &d.ExpiresAt, &d.LastError)
	return d, normalize(e)
}
func (s *Store) AnycastDispatch(ctx context.Context, a Actor, id string) (AnycastDispatch, error) {
	d, e := scanAnycast(s.Pool.QueryRow(ctx, `SELECT `+anycastFields+` FROM anycast_dispatches d LEFT JOIN groups g ON g.scope=d.scope AND g.id=d.role_id WHERE d.scope=$1 AND d.id=$2 AND (d.requester_id=$3 OR $4)`, a.Scope, id, a.ID, a.Admin))
	return d, e
}
func (s *Store) AnycastDispatches(ctx context.Context, a Actor) ([]AnycastDispatch, error) {
	// Polling the workspace list needs previews, not full task bodies or actions.
	fields := strings.Replace(anycastFields, "d.input", `jsonb_build_object('role_id',d.role_id,'kind',d.input->>'kind','title',d.input->>'title','body',left(d.input->>'body',240),'expires_at',d.input->>'expires_at')`, 1)
	rows, e := s.Pool.Query(ctx, `SELECT `+fields+` FROM anycast_dispatches d LEFT JOIN groups g ON g.scope=d.scope AND g.id=d.role_id WHERE d.scope=$1 AND d.requester_id=$2 ORDER BY d.created_at DESC,d.id DESC LIMIT 100`, a.Scope, a.ID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AnycastDispatch{}
	for rows.Next() {
		d, e := scanAnycast(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func anycastAction(in AnycastInput) (string, string) {
	if in.Kind == "" || in.Kind == "message" {
		return "message", "message"
	}
	return "ask", "request"
}
func (s *Store) Anycast(ctx context.Context, a Actor, in AnycastInput, key string) (AnycastDispatch, error) {
	var out AnycastDispatch
	_, permission := anycastAction(in)
	ctx = WithCredentialPermission(ctx, permission)
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if a.CredentialID == "" || a.CredentialKind == "enrollment" {
			return Denied()
		}
		return idempotent(ctx, tx, a, "anycast", key, in, &out, func() error {
			if len(JSON(in)) > 65536 {
				return Bad("Dispatch input must be at most 64 KiB. Use attachments for larger content.")
			}
			var pending int
			if e := tx.QueryRow(ctx, `SELECT count(*) FROM anycast_dispatches WHERE scope=$1 AND requester_id=$2 AND state='queued'`, a.Scope, a.ID).Scan(&pending); e != nil {
				return e
			}
			if pending >= 100 {
				return &Error{"QUEUE_FULL", "You already have 100 queued dispatches. Wait for capacity or cancel one before sending more.", 429}
			}
			normalized := in
			if normalized.Kind == "" {
				normalized.Kind = "message"
			}
			if !ValidText(in.Body, 32768) && !(in.Body == "" && len(in.AttachmentIDs) > 0) {
				return Bad("A message of at most 32768 bytes or an attachment is required.")
			}
			if len(in.Title) > 200 || len(in.Choices) > 20 {
				return Bad("Use a title of at most 200 bytes and up to 20 choices.")
			}
			switch normalized.Kind {
			case "message", "question", "task", "approval":
			default:
				return Bad("kind must be message, question, task or approval.")
			}
			if normalized.Kind != "question" && len(in.Choices) > 0 {
				return Bad("Choices require a question.")
			}
			for _, c := range in.Choices {
				if !ValidText(c, 500) {
					return Bad("Choices must contain 1 to 500 bytes.")
				}
			}
			if in.Action != nil && (in.Kind != "approval" || !ValidText(in.Action.Name, 120) || in.Action.ExecutorID == "" || in.Action.Resource == "" || in.Action.ResourceVersion == "" || in.Action.OperationKey == "") {
				return Bad("A bound approval requires an exact action, executor, resource version and operation key.")
			}
			if normalized.ExpiresAt.IsZero() {
				normalized.ExpiresAt = time.Now().UTC().Add(24 * time.Hour)
			}
			if !normalized.ExpiresAt.After(time.Now()) || normalized.ExpiresAt.After(time.Now().Add(90*24*time.Hour)) {
				return Bad("Expiry must be in the next 90 days.")
			}
			if _, e := s.anycastRecipients(ctx, tx, a, normalized); e != nil {
				return e
			}
			out = AnycastDispatch{ID: ID("acast"), RoleID: in.RoleID, RequesterID: a.ID, State: "queued", Input: normalized, CreatedAt: time.Now(), ExpiresAt: normalized.ExpiresAt}
			bound := withAttachmentBinding(ctx, "anycast", out.ID)
			if _, e := s.claimAttachments(bound, tx, a, in.AttachmentIDs); e != nil {
				return e
			}
			_, e := tx.Exec(ctx, `INSERT INTO anycast_dispatches(id,scope,requester_id,credential_id,role_id,input,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, out.ID, a.Scope, a.ID, a.CredentialID, in.RoleID, JSON(normalized), normalized.ExpiresAt)
			if e != nil {
				return e
			}
			return event(ctx, tx, a, "anycast.queued", "", a.ID, map[string]string{"id": out.ID})
		})
	})
	if err != nil {
		return out, err
	}
	// The dispatch is durable even if a separate routing attempt fails. The
	// existing maintenance worker retries; callers can always recover by its ID.
	_ = s.tx(ctx, a, func(tx pgx.Tx) error { return s.routeAnycast(ctx, tx, a.Scope) })
	return s.AnycastDispatch(ctx, a, out.ID)
}

func (s *Store) anycastRecipients(ctx context.Context, tx pgx.Tx, a Actor, in AnycastInput) ([]recipient, error) {
	ctx = withPolicySnapshot(ctx)
	var role bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE scope=$1 AND id=$2 AND purpose='agent_role')`, a.Scope, in.RoleID).Scan(&role); e != nil {
		return nil, e
	}
	if !role {
		return nil, Bad("Choose an agent role for Anycast.")
	}
	action, permission := anycastAction(in)
	if !a.CredentialPermits(permission) {
		return nil, Denied()
	}
	if in.Action != nil {
		executor, e := principal(ctx, tx, a.Scope, in.Action.ExecutorID)
		if e != nil || executor.Disabled || !s.allowed(ctx, tx, a, "ask", Target{executor.Kind, executor.ID}, "*", in.Action.Name, in.Action) {
			return nil, Denied()
		}
	}
	rs, e := s.resolve(ctx, tx, a, Target{"group", in.RoleID}, action, "", in.Action)
	if e != nil {
		return nil, e
	}
	eligible := rs[:0]
	for _, r := range rs {
		if r.Kind == "agent" && r.ID != a.ID && !s.recipientBlocked(ctx, tx, a, action, r.ID) && (action == "message" || s.requestRecipientCanRespond(ctx, tx, a, r.Principal, in.Kind, in.Action)) {
			eligible = append(eligible, r)
		}
	}
	if len(eligible) == 0 {
		return nil, &Error{"NO_ELIGIBLE_RECIPIENT", "No permitted agent in this role can receive this dispatch.", 403}
	}
	return eligible, nil
}

// Reload current authority rather than persisting an admin flag or token.
func anycastSender(ctx context.Context, tx pgx.Tx, scope, credential string) (Actor, error) {
	a := Actor{Scope: scope, CredentialID: credential}
	e := tx.QueryRow(ctx, `SELECT p.id,p.kind,c.kind,c.permissions,COALESCE(i.id,''),
 c.kind='admin' OR (c.kind='browser' AND EXISTS(SELECT 1 FROM credentials ac WHERE ac.scope=c.scope AND ac.principal_id=p.id AND ac.kind='admin' AND NOT ac.revoked AND (ac.expires_at IS NULL OR ac.expires_at>now())))
 FROM credentials c JOIN principals p ON p.scope=c.scope AND p.id=c.principal_id LEFT JOIN instances i ON i.scope=c.scope AND i.credential_id=c.id
 WHERE c.scope=$1 AND c.id=$2 AND NOT c.revoked AND NOT p.disabled AND (c.expires_at IS NULL OR c.expires_at>now())`, scope, credential).Scan(&a.ID, &a.Kind, &a.CredentialKind, &a.TokenPermissions, &a.InstanceID, &a.Admin)
	return a, e
}

// All routing and capacity changes share the existing workspace transaction
// fence. Readers and ordinary sends retain their independent paths.
const anycastReadyRolesSQL = `SELECT DISTINCT i.scope,m.group_id FROM instances i
 JOIN memberships m ON m.scope=i.scope AND m.principal_id=i.agent_id
 JOIN credentials c ON c.scope=i.scope AND c.id=i.credential_id
 WHERE i.anycast_capacity>0 AND i.anycast_until>now() AND NOT c.revoked
 AND (c.expires_at IS NULL OR c.expires_at>now()) AND (c.permissions IS NULL OR 'read'=ANY(c.permissions))
 AND i.anycast_capacity>(SELECT count(*) FROM anycast_dispatches d WHERE d.scope=i.scope AND d.instance_id=i.id AND d.state IN ('assigned','accepted'))`

func (s *Store) routeAnycast(ctx context.Context, tx pgx.Tx, scope string) error {
	rows, e := tx.Query(ctx, `WITH ready_roles AS MATERIALIZED (`+anycastReadyRolesSQL+` AND i.scope=$1)
 SELECT id,credential_id,input FROM anycast_dispatches d WHERE scope=$1 AND state='queued'
 AND (expires_at<=now() OR role_id IN (SELECT group_id FROM ready_roles)) ORDER BY updated_at,created_at,id LIMIT 32`, scope)
	if e != nil {
		return e
	}
	type pending struct {
		id, credential string
		input          AnycastInput
	}
	jobs := []pending{}
	for rows.Next() {
		var j pending
		if e = rows.Scan(&j.id, &j.credential, jsonColumn{&j.input}); e != nil {
			rows.Close()
			return e
		}
		jobs = append(jobs, j)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, j := range jobs {
		// A policy or quota failure in one dispatch must not starve another sender.
		sub, e := tx.Begin(ctx)
		if e != nil {
			return e
		}
		sender, err := anycastSender(ctx, sub, scope, j.credential)
		if errors.Is(err, pgx.ErrNoRows) {
			err = Denied()
		}
		state := "blocked"
		if !j.input.ExpiresAt.After(time.Now()) {
			state = "expired"
			err = Bad("Dispatch expired before an agent was available.")
		}
		var recipients []recipient
		if err == nil {
			recipients, err = s.anycastRecipients(ctx, sub, sender, j.input)
		}
		if err == nil {
			err = s.assignAnycast(ctx, sub, sender, j.id, j.input, recipients)
		}
		if err == nil {
			if _, e = sub.Exec(ctx, `UPDATE anycast_dispatches SET updated_at=now() WHERE scope=$1 AND id=$2 AND state='queued'`, scope, j.id); e != nil {
				_ = sub.Rollback(ctx)
				return e
			}
			if e = sub.Commit(ctx); e != nil {
				return e
			}
			continue
		}
		_ = sub.Rollback(ctx)
		var domain *Error
		if !errors.As(err, &domain) {
			return err
		}
		if _, e = tx.Exec(ctx, `UPDATE anycast_dispatches SET state=$3,last_error=$4,updated_at=now() WHERE scope=$1 AND id=$2`, scope, j.id, state, domain.Message); e != nil {
			return e
		}
		if e = s.releaseAnycastFiles(ctx, tx, scope, j.id); e != nil {
			return e
		}
	}
	return nil
}

func (s *Store) assignAnycast(ctx context.Context, tx pgx.Tx, a Actor, id string, in AnycastInput, rs []recipient) error {
	ids := make([]string, 0, len(rs))
	byID := map[string]recipient{}
	for _, r := range rs {
		ids = append(ids, r.ID)
		byID[r.ID] = r
	}
	var agent, instance string
	e := tx.QueryRow(ctx, `SELECT i.agent_id,i.id FROM instances i JOIN credentials c ON c.id=i.credential_id AND c.scope=i.scope
 LEFT JOIN LATERAL (SELECT count(*) n FROM anycast_dispatches d WHERE d.scope=i.scope AND d.instance_id=i.id AND d.state IN ('assigned','accepted')) used ON true
 WHERE i.scope=$1 AND i.agent_id=ANY($2) AND i.anycast_until>now() AND i.anycast_capacity>used.n
 AND NOT c.revoked AND (c.expires_at IS NULL OR c.expires_at>now())
 AND (c.permissions IS NULL OR ('read'=ANY(c.permissions) AND ($3='message' OR 'respond'=ANY(c.permissions))))
 ORDER BY used.n::float/i.anycast_capacity,i.anycast_last_assigned NULLS FIRST,i.id LIMIT 1`, a.Scope, ids, in.Kind).Scan(&agent, &instance)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	chosen := byID[agent]
	chosen.ExactInstance = instance
	ctx = context.WithValue(withAttachmentBinding(ctx, "anycast", id), anycastContextKey{}, anycastSelection{id, in.RoleID, chosen})
	conv, request := "", ""
	operations := []string{}
	if in.Kind == "message" {
		var out SendResult
		e = s.sendTx(ctx, tx, a, SendInput{Target: Target{"group", in.RoleID}, Body: in.Body, Title: in.Title, AttachmentIDs: in.AttachmentIDs}, id, &out)
		conv = out.ConversationID
		for _, message := range out.Messages {
			operations = append(operations, message.ID)
		}
	} else {
		var out Request
		e = s.askTx(ctx, tx, a, AskInput{Target: Target{"group", in.RoleID}, Body: in.Body, Title: in.Title, Kind: in.Kind, Choices: in.Choices, Action: in.Action, AttachmentIDs: in.AttachmentIDs, ExpiresAt: in.ExpiresAt}, id, &out)
		conv, request = out.ConversationID, out.ID
		operations = append(operations, out.ID)
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE deliveries SET payload=payload||jsonb_build_object('anycast_id',$3::text,'anycast_role_id',$4::text),expires_at=LEAST(expires_at,$5) WHERE scope=$1 AND operation_id=ANY($2)`, a.Scope, operations, id, in.RoleID, in.ExpiresAt); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE instances SET anycast_last_assigned=now() WHERE scope=$1 AND id=$2`, a.Scope, instance); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE anycast_dispatches SET state='assigned',agent_id=$3,instance_id=$4,conversation_id=$5,request_id=$6,updated_at=now() WHERE scope=$1 AND id=$2`, a.Scope, id, agent, instance, conv, request); e != nil {
		return e
	}
	return event(ctx, tx, a, "anycast.assigned", conv, a.ID, map[string]string{"id": id, "agent_id": agent})
}

func (s *Store) releaseAnycastFiles(ctx context.Context, tx pgx.Tx, scope, id string) error {
	_, e := tx.Exec(ctx, `WITH released AS (UPDATE attachments SET bound_operation='',draft_until=now() WHERE scope=$1 AND bound_operation=$2 AND NOT EXISTS(SELECT 1 FROM attachment_links l WHERE l.scope=attachments.scope AND l.attachment_id=attachments.id) RETURNING id,scope)
 INSERT INTO attachment_uploads(id,scope,lease_until,next_at) SELECT id,scope,now(),now() FROM released ON CONFLICT DO NOTHING`, scope, Digest("anycast/"+id))
	return e
}
func (s *Store) CancelAnycast(ctx context.Context, a Actor, id string) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		var state string
		if e := tx.QueryRow(ctx, `SELECT state FROM anycast_dispatches WHERE scope=$1 AND id=$2 AND (requester_id=$3 OR $4)`, a.Scope, id, a.ID, a.Admin).Scan(&state); e != nil {
			return normalize(e)
		}
		if state == "cancelled" {
			return nil
		}
		if state != "queued" {
			return Conflict("This dispatch has already left the queue. Follow up in its conversation.")
		}
		if _, e := tx.Exec(ctx, `UPDATE anycast_dispatches SET state='cancelled',updated_at=now() WHERE scope=$1 AND id=$2`, a.Scope, id); e != nil {
			return e
		}
		return s.releaseAnycastFiles(ctx, tx, a.Scope, id)
	})
}

// Completion is a host lifecycle signal. ACK alone never releases capacity.
func (s *Store) CompleteAnycast(ctx context.Context, a Actor, id string) error {
	if a.InstanceID == "" {
		return Denied()
	}
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE anycast_dispatches SET state='completed',updated_at=now() WHERE scope=$1 AND id=$2 AND instance_id=$3 AND state IN ('accepted','completed')`, a.Scope, id, a.InstanceID)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return Conflict("Only the assigned runtime can finish an accepted dispatch.")
		}
		return s.routeAnycast(ctx, tx, a.Scope)
	})
}

// Reuses the maintenance loop. No additional process or per-agent poller.
func (s *Store) ProcessAnycast(ctx context.Context) (bool, error) {
	var scope string
	e := s.Pool.QueryRow(ctx, `WITH ready_roles AS MATERIALIZED (`+anycastReadyRolesSQL+`)
 SELECT d.scope FROM anycast_dispatches d JOIN scopes s ON s.id=d.scope WHERE d.state IN ('queued','assigned','accepted') AND NOT s.suspended AND NOT s.recovery_hold AND (s.access_until IS NULL OR s.access_until>now()) AND (
 (d.state='queued' AND (d.expires_at<=now() OR EXISTS(SELECT 1 FROM ready_roles r WHERE r.scope=d.scope AND r.group_id=d.role_id)))
 OR (d.state='assigned' AND d.expires_at<=now())
 OR (d.state IN ('assigned','accepted') AND EXISTS(SELECT 1 FROM requests r WHERE r.scope=d.scope AND r.id=d.request_id AND r.state='resolved')))
 ORDER BY d.updated_at,d.id LIMIT 1`).Scan(&scope)
	if e == pgx.ErrNoRows {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	e = s.tx(ctx, Actor{Scope: scope}, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `UPDATE anycast_dispatches SET state='expired',updated_at=now() WHERE scope=$1 AND state='assigned' AND expires_at<=now()`, scope); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `UPDATE anycast_dispatches d SET state='completed',updated_at=now() WHERE d.scope=$1 AND d.state IN ('assigned','accepted') AND EXISTS(SELECT 1 FROM requests r WHERE r.scope=d.scope AND r.id=d.request_id AND r.state='resolved')`, scope); e != nil {
			return e
		}
		return s.routeAnycast(ctx, tx, scope)
	})
	return true, e
}

func parseAnycastAvailability(progress map[string]any) (*AnycastAvailability, error) {
	raw, ok := progress["anycast"]
	if !ok {
		return nil, nil
	}
	var out struct {
		Capacity *int `json:"capacity"`
	}
	if raw == nil || json.Unmarshal(JSON(raw), &out) != nil || out.Capacity == nil || *out.Capacity < 0 || *out.Capacity > 100 {
		return nil, Bad("anycast.capacity must be an integer from 0 to 100.")
	}
	return &AnycastAvailability{Capacity: *out.Capacity}, nil
}
