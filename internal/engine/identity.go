package engine

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const principalFields = `id,kind,external_id,name,metadata,disabled,created_at`

func scanPrincipal(row pgx.Row) (Principal, error) {
	var p Principal
	err := row.Scan(&p.ID, &p.Kind, &p.ExternalID, &p.Name, jsonColumn{&p.Metadata}, &p.Disabled, &p.CreatedAt)
	return p, normalize(err)
}
func principal(ctx context.Context, db DB, scope, id string) (Principal, error) {
	return scanPrincipal(db.QueryRow(ctx, `SELECT `+principalFields+` FROM principals WHERE scope=$1 AND id=$2`, scope, id))
}
func (s *Store) Me(ctx context.Context, a Actor) (map[string]any, error) {
	var company, env string
	err := s.Pool.QueryRow(ctx, `SELECT company_name,environment FROM scopes WHERE id=$1`, a.Scope).Scan(&company, &env)
	result := map[string]any{"identity_type": a.Kind, "instance_id": a.InstanceID, "admin": a.Admin, "can_publish_answers": s.canPublish(ctx, s.Pool, a), "company": company, "environment": env}
	result[a.Kind] = a.Principal
	return result, err
}
func (s *Store) CreatePerson(ctx context.Context, a Actor, in CreatePerson) (Principal, error) {
	if !ValidText(in.Name, 120) || !ValidText(in.ExternalID, 200) {
		return Principal{}, Bad("Name and external_id are required.")
	}
	p := Principal{ID: ID("per"), Kind: "person", ExternalID: in.ExternalID, Name: in.Name, Metadata: map[string]any{"department": in.Department, "manager_id": in.ManagerID, "email": in.Email}, CreatedAt: time.Now()}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.allowed(ctx, tx, a, "manage", Target{"person", "*"}, "", "") {
			return Denied()
		}
		if in.ManagerID != "" {
			m, e := principal(ctx, tx, a.Scope, in.ManagerID)
			if e != nil || m.Kind != "person" {
				return Bad("Manager must be a person in this workspace.")
			}
		}
		if _, e := tx.Exec(ctx, `INSERT INTO principals(id,scope,kind,external_id,name,metadata) VALUES($1,$2,$3,$4,$5,$6)`, p.ID, a.Scope, p.Kind, p.ExternalID, p.Name, JSON(p.Metadata)); e != nil {
			return e
		}
		return event(ctx, tx, a, "person.created", "", p.ID, map[string]any{"id": p.ID})
	})
	return p, err
}
func (s *Store) ListPrincipals(ctx context.Context, a Actor, kind string) ([]Principal, error) {
	return s.listPrincipals(ctx, s.Pool, a, kind)
}
func (s *Store) listPrincipals(ctx context.Context, db DB, a Actor, kind string) ([]Principal, error) {
	ctx = withPolicySnapshot(ctx)
	if kind != "person" && kind != "agent" && kind != "service" {
		return nil, Bad("Invalid collection.")
	}
	rows, err := db.Query(ctx, `SELECT `+principalFields+` FROM principals WHERE scope=$1 AND kind=$2 ORDER BY created_at,id LIMIT 1000`, a.Scope, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all := []Principal{}
	for rows.Next() {
		p, e := scanPrincipal(rows)
		if e != nil {
			return nil, e
		}
		all = append(all, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	visible := []Principal{}
	for _, p := range all {
		if s.inspectIdentity(ctx, db, a, p) {
			visible = append(visible, p)
		}
	}
	return visible, nil
}
func (s *Store) SetDisabled(ctx context.Context, a Actor, id string, disabled bool) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		p, e := principal(ctx, tx, a.Scope, id)
		if e != nil {
			return e
		}
		if id == a.ID {
			return Bad("You cannot disable your own account.")
		}
		if !s.managesIdentity(ctx, tx, a, p) {
			return Denied()
		}
		if _, e = tx.Exec(ctx, `UPDATE principals SET disabled=$3 WHERE scope=$1 AND id=$2`, a.Scope, id, disabled); e != nil {
			return e
		}
		return event(ctx, tx, a, "principal.updated", "", id, map[string]any{"id": id, "disabled": disabled})
	})
}
func (s *Store) NewCredential(ctx context.Context, a Actor, pid, kind, prefix string, options ...CredentialOptions) (map[string]any, error) {
	var opt CredentialOptions
	if len(options) > 0 {
		opt = options[0]
	}
	if len(strings.TrimSpace(opt.Name)) > 120 {
		return nil, Bad("Token name must be at most 120 characters.")
	}
	if opt.NeverExpires && opt.ExpiresAt != nil {
		return nil, Bad("Choose an expiry date or never expires, not both.")
	}
	var expires *time.Time
	var ttl time.Duration
	if !opt.NeverExpires {
		until, e := credentialExpiry(opt.ExpiresAt, 30*24*time.Hour)
		if e != nil {
			return nil, e
		}
		expires, ttl = &until, time.Until(until)
	}
	permissions, e := credentialPermissions(opt.Permissions)
	if e != nil {
		return nil, e
	}
	if kind != "api" && permissions != nil {
		return nil, Bad("Permission limits are available for API tokens.")
	}
	var token, id string
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		p, e := principal(ctx, tx, a.Scope, pid)
		if e != nil {
			return e
		}
		if !s.mayIssueCredential(ctx, tx, a, p) {
			return Denied()
		}
		if kind != "api" && kind != "enrollment" {
			return Bad("Credential kind must be api or enrollment.")
		}
		if p.Disabled {
			return Denied()
		}
		if kind == "enrollment" && !ValidText(prefix, 120) {
			return Bad("An enrollment namespace prefix is required.")
		}
		token, id, e = issue(ctx, tx, a, pid, kind, prefix, ttl)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE credentials SET name=$3,permissions=$4,expires_at=$5 WHERE scope=$1 AND id=$2`, a.Scope, id, strings.TrimSpace(opt.Name), permissions, expires); e != nil {
			return e
		}
		return event(ctx, tx, a, "credential.created", "", pid, map[string]any{"id": id, "kind": kind})
	})
	return map[string]any{"id": id, "token": token, "kind": kind, "expires_at": expires}, err
}
func (s *Store) Credentials(ctx context.Context, a Actor) ([]Credential, error) {

	rows, e := s.Pool.Query(ctx, `SELECT `+credentialFields+` FROM credentials WHERE scope=$1 ORDER BY created_at DESC LIMIT 1000`, a.Scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		c, err := scanCredential(rows)
		if e = err; e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	visible := []Credential{}
	for _, c := range out {
		if a.Admin || c.PrincipalID == a.ID || s.manages(ctx, s.Pool, a, "credential", c.PrincipalID) {
			visible = append(visible, c)
		}
	}
	return visible, nil
}
func (s *Store) Revoke(ctx context.Context, a Actor, id string, revokeWorkers ...bool) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		var pid, kind string
		if e := tx.QueryRow(ctx, `SELECT principal_id,kind FROM credentials WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&pid, &kind); e != nil {
			return normalize(e)
		}
		p, e := principal(ctx, tx, a.Scope, pid)
		if e != nil {
			return e
		}
		if !a.Admin && (kind == "admin" || (pid != a.ID && !s.mayIssueCredential(ctx, tx, a, p))) {
			return Denied()
		}
		tag, e := tx.Exec(ctx, `UPDATE credentials SET revoked=true,version=version+1 WHERE scope=$1 AND id=$2`, a.Scope, id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return Missing()
		}
		if len(revokeWorkers) > 0 && revokeWorkers[0] {
			if kind != "enrollment" {
				return Bad("Only enrollment tokens have issued worker connections.")
			}
			if _, e = tx.Exec(ctx, `UPDATE credentials SET revoked=true,version=version+1 WHERE scope=$1 AND parent_credential_id=$2 AND kind='worker'`, a.Scope, id); e != nil {
				return e
			}
		}
		return event(ctx, tx, a, "credential.revoked", "", a.ID, map[string]any{"id": id, "workers_revoked": len(revokeWorkers) > 0 && revokeWorkers[0]})
	})
}
func (s *Store) BrowserSession(ctx context.Context, a Actor) (string, error) {
	if a.Kind != "person" || a.CredentialKind == "enrollment" || a.TokenPermissions != nil {
		return "", Denied()
	}
	var token string
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
		var e error
		token, _, e = issue(ctx, tx, a, a.ID, "browser", "", 12*time.Hour)
		return e
	})
	return token, e
}
func (s *Store) Logout(ctx context.Context, a Actor) error {
	if a.CredentialKind != "browser" {
		return nil
	}
	_, err := s.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE scope=$1 AND id=$2`, a.Scope, a.CredentialID)
	return err
}

func (s *Store) Enroll(ctx context.Context, a Actor, in EnrollInput) (Session, error) {
	var out Session
	if !a.CredentialPermits("manage") {
		return out, Denied()
	}
	fallback := 24 * time.Hour
	if in.Renewable {
		fallback = 30 * 24 * time.Hour
	}
	if in.NeverExpires && (in.ExpiresAt != nil || in.Renewable) {
		return out, Bad("A connection that never expires does not need renewal or an expiry date.")
	}
	var deadline time.Time
	if !in.NeverExpires {
		var e error
		deadline, e = credentialExpiry(in.ExpiresAt, fallback)
		if e != nil {
			return out, e
		}
	}
	if len(strings.TrimSpace(in.ConnectionName)) > 120 {
		return out, Bad("Connection name must be at most 120 characters.")
	}
	if !ValidText(in.Agent, 200) || !ValidText(in.Name, 120) || len(in.Capabilities) > 32 {
		return out, Bad("agent and name are required; at most 32 capabilities.")
	}
	if !a.Admin && a.CredentialKind != "enrollment" {
		return out, Denied()
	}
	if !a.Admin && !strings.HasPrefix(in.Agent, a.Prefix) {
		return out, Denied()
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		var count int
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM instances WHERE scope=$1 AND created_at>now()-interval '1 hour'`, a.Scope).Scan(&count); e != nil {
			return e
		}
		if count >= 10000 {
			return &Error{"RATE_LIMITED", "Enrollment limit reached.", 429}
		}
		if in.ParentID != "" {
			parent, e := principal(ctx, tx, a.Scope, in.ParentID)
			if e != nil || parent.Kind != "agent" || (!a.Admin && parent.Metadata["enrolled_by"] != a.ID) {
				return Denied()
			}
		}
		p, e := scanPrincipal(tx.QueryRow(ctx, `SELECT `+principalFields+` FROM principals WHERE scope=$1 AND kind='agent' AND external_id=$2`, a.Scope, in.Agent))
		if e != nil && ErrorStatus(e) != 404 {
			return e
		}
		if e != nil {
			meta := map[string]any{"enrolled_by": a.ID, "parent_id": in.ParentID}
			for _, k := range []string{"project", "type", "description"} {
				if v, ok := in.Metadata[k]; ok {
					label, ok := v.(string)
					if !ok || len(label) > 512 {
						return Bad("Agent labels must be bounded text.")
					}
					meta[k] = label
				}
			}
			p = Principal{ID: ID("agt"), Kind: "agent", ExternalID: in.Agent, Name: in.Name, Metadata: meta, CreatedAt: time.Now()}
			if _, e = tx.Exec(ctx, `INSERT INTO principals(id,scope,kind,external_id,name,metadata) VALUES($1,$2,'agent',$3,$4,$5)`, p.ID, a.Scope, p.ExternalID, p.Name, JSON(meta)); e != nil {
				return e
			}
			// Copy only explicit role assignments. Administrative credential authority is never inherited.
			if _, e = tx.Exec(ctx, `INSERT INTO role_bindings(scope,role_id,subject_kind,subject_id) SELECT scope,role_id,'principal',$3 FROM role_bindings WHERE scope=$1 AND subject_kind='principal' AND subject_id=$2 ON CONFLICT DO NOTHING`, a.Scope, a.ID, p.ID); e != nil {
				return e
			}
		} else if p.Disabled || (!a.Admin && p.Metadata["enrolled_by"] != a.ID) {
			return Denied()
		}
		lease := deadline
		if in.Renewable && lease.After(time.Now().Add(24*time.Hour)) {
			lease = time.Now().Add(24 * time.Hour)
		}
		var ttl time.Duration
		var expires *time.Time
		if !in.NeverExpires {
			ttl, expires = time.Until(lease), &lease
		}
		token, cid, e := issue(ctx, tx, a, p.ID, "worker", "", ttl)
		if e != nil {
			return e
		}
		var renewable *time.Time
		var parent *string
		if in.Renewable {
			renewable = &deadline
		}
		if a.CredentialKind == "enrollment" {
			parent = &a.CredentialID
		}
		if _, e = tx.Exec(ctx, `UPDATE credentials SET name=$3,renewable_until=$4,parent_credential_id=$5,expires_at=$6 WHERE scope=$1 AND id=$2`, a.Scope, cid, strings.TrimSpace(in.ConnectionName), renewable, parent, expires); e != nil {
			return e
		}
		instance := ID("ins")
		if _, e = tx.Exec(ctx, `INSERT INTO instances(id,scope,agent_id,credential_id,capabilities) VALUES($1,$2,$3,$4,$5)`, instance, a.Scope, p.ID, cid, JSON(in.Capabilities)); e != nil {
			return e
		}
		out = Session{token, p, instance, expires}
		return event(ctx, tx, a, "agent.connected", "", p.ID, map[string]any{"agent_id": p.ID, "instance_id": instance})
	})
	return out, err
}
func (s *Store) Instances(ctx context.Context, a Actor, agentID string) ([]Instance, error) {
	if !s.allowed(ctx, s.Pool, a, "view", Target{"agent", agentID}, "", "") && a.ID != agentID {
		return nil, Denied()
	}
	rows, e := s.Pool.Query(ctx, `SELECT i.id,i.agent_id,i.capabilities,i.progress,i.last_seen,CASE WHEN c.revoked OR c.expires_at<=now() OR EXISTS(SELECT 1 FROM principals p WHERE p.scope=i.scope AND p.id=i.agent_id AND p.disabled) THEN 'offline' WHEN i.last_seen>now()-interval '45 seconds' THEN 'connected' WHEN i.last_seen>now()-interval '2 minutes' THEN 'stale' ELSE 'offline' END FROM instances i JOIN credentials c ON c.id=i.credential_id WHERE i.scope=$1 AND i.agent_id=$2 ORDER BY i.created_at DESC LIMIT 1000`, a.Scope, agentID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Instance{}
	for rows.Next() {
		var x Instance
		var caps, progress []byte
		if e = rows.Scan(&x.ID, &x.AgentID, &caps, &progress, &x.LastSeen, &x.Presence); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(caps, &x.Capabilities)
		_ = json.Unmarshal(progress, &x.Progress)
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) Heartbeat(ctx context.Context, a Actor, progress map[string]any) error {
	if a.InstanceID == "" {
		return Denied()
	}
	if len(JSON(progress)) > 8192 {
		return Bad("Progress payload too large.")
	}
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE instances SET last_seen=now(),progress=$3 WHERE scope=$1 AND id=$2 AND credential_id=$4`, a.Scope, a.InstanceID, JSON(progress), a.CredentialID)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return Denied()
		}
		return nil
	})
}

func (s *Store) Agents(ctx context.Context, a Actor) ([]Agent, error) {
	// Identity filtering and runtime visibility belong to the same bounded read.
	ctx = withPolicySnapshot(ctx)
	principals, e := s.ListPrincipals(ctx, a, "agent")
	if e != nil {
		return nil, e
	}
	ids := make([]string, 0, len(principals))
	for _, p := range principals {
		if p.ID == a.ID || s.allowed(ctx, s.Pool, a, "view", Target{"agent", p.ID}, "", "") {
			ids = append(ids, p.ID)
		}
	}
	byAgent, e := s.instancesForAgents(ctx, a.Scope, ids)
	if e != nil {
		return nil, e
	}
	out := make([]Agent, 0, len(principals))
	for _, p := range principals {
		out = append(out, Agent{p, byAgent[p.ID]})
	}

	return out, nil
}

func (s *Store) UpdatePerson(ctx context.Context, a Actor, id string, in CreatePerson) (Principal, error) {
	var out Principal
	if !ValidText(in.Name, 120) || len(in.Department) > 200 || len(in.Email) > 320 {
		return out, Bad("A bounded name, email and department are required.")
	}
	e := s.tx(ctx, a, func(tx pgx.Tx) error {

		p, e := principal(ctx, tx, a.Scope, id)
		if e != nil || p.Kind != "person" {
			return Missing()
		}
		if !s.managesIdentity(ctx, tx, a, p) {
			return Denied()
		}
		next := in.ManagerID
		visited := map[string]bool{id: true}
		for next != "" {
			if visited[next] {
				return Bad("Manager relationships cannot contain a cycle.")
			}
			visited[next] = true
			m, e := principal(ctx, tx, a.Scope, next)
			if e != nil || m.Kind != "person" {
				return Bad("Manager must be a person in this workspace.")
			}
			next, _ = m.Metadata["manager_id"].(string)
		}
		_, e = tx.Exec(ctx, `UPDATE principals SET name=$3,metadata=metadata||$4::jsonb WHERE scope=$1 AND id=$2`, a.Scope, id, in.Name, JSON(map[string]string{"email": in.Email, "department": in.Department, "manager_id": in.ManagerID}))
		if e != nil {
			return e
		}
		out, e = principal(ctx, tx, a.Scope, id)
		if e != nil {
			return e
		}
		return event(ctx, tx, a, "person.updated", "", id, map[string]string{"id": id})
	})
	return out, e
}

func (s *Store) SetResourceDisabled(ctx context.Context, a Actor, kind, id string, disabled bool) error {
	p, e := principal(ctx, s.Pool, a.Scope, id)
	if e != nil || p.Kind != kind {
		return Missing()
	}
	return s.SetDisabled(ctx, a, id, disabled)
}
