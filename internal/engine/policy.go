package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"math"
	"strings"
	"time"
)

func (s *Store) allowed(ctx context.Context, db DB, a Actor, action string, target Target, connection, actionName string, descriptor ...*Action) bool {
	if a.CredentialKind == "enrollment" {
		return false
	}
	if a.Admin {
		return true
	}
	all, e := roleRules(ctx, db, a)
	if e != nil {
		return false
	}

	for _, r := range all {
		if s.ruleAllows(ctx, db, a, r, action, target, connection, actionName, descriptor...) {
			return true
		}
	}

	return false
}
func (s *Store) ruleAllows(ctx context.Context, db DB, a Actor, r Rule, action string, target Target, connection, actionName string, descriptor ...*Action) bool {
	if r.Action != action && r.Action != "*" {
		return false
	}
	groupMember := false
	if r.TargetKind == "group" && (target.Kind == "person" || target.Kind == "agent" || target.Kind == "service") && target.ID != "*" {
		groupMember = targetGroupMember(ctx, db, a.Scope, r.TargetID, target.ID)
	}
	if !groupMember && r.TargetKind != "*" && r.TargetKind != target.Kind {
		return false
	}
	if !groupMember && r.TargetID != "*" && r.TargetID != target.ID && !(r.TargetID == "$self" && target.ID == a.ID) {
		return false
	}
	if r.ConnectionID != "" && r.ConnectionID != connection && connection != "*" {
		return false
	}
	if r.ActionName != "" && r.ActionName != actionName {
		return false
	}
	var detail *Action
	if len(descriptor) > 0 {
		detail = descriptor[0]
	}
	if !conditionsAllow(r.Conditions, detail) {
		return false
	}
	return true
}
func (s *Store) Explain(ctx context.Context, a Actor, action string, target Target, connection, actionName string, descriptor ...*Action) (map[string]any, error) {
	if a.CredentialKind == "enrollment" {
		return nil, Denied()
	}
	switch action {
	case "view", "message", "ask", "decide", "manage":
	default:
		return nil, Bad("Unsupported permission action.")
	}
	administrative := false
	if action == "manage" {
		_, administrative = PermissionCatalog()["management_targets"].(map[string]string)[target.Kind]
	}
	if action == "view" {
		switch target.Kind {
		case "role", "access", "account", "connection", "destination", "delivery", "answer":
			administrative = true
		}
	}
	if administrative {
		if !ValidText(target.ID, 256) {
			return nil, Bad("A resource ID is required.")
		}
	} else {
		if err := target.Validate(); err != nil {
			return nil, err
		}
		if target.Kind != "group" && target.Kind != "instance" {
			p, e := principal(ctx, s.Pool, a.Scope, target.ID)
			if e != nil || p.Disabled {
				return map[string]any{"allowed": false, "reason": "Target unavailable or not permitted."}, nil
			}
		}
	}
	allowed := s.allowed(ctx, s.Pool, a, action, target, connection, actionName, descriptor...)
	if action == "manage" {
		allowed = s.manages(ctx, s.Pool, a, target.Kind, target.ID)
	}
	return map[string]any{"allowed": allowed, "action": action, "target": target, "reason": map[bool]string{true: "An effective role or administrator credential permits this operation.", false: "No effective permission permits this operation."}[allowed]}, nil
}
func (s *Store) Roles(ctx context.Context, a Actor) ([]Role, error) {

	rows, e := s.Pool.Query(ctx, `SELECT id,name,rules,version FROM roles WHERE scope=$1 ORDER BY name`, a.Scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		var r Role
		var raw []byte
		if e = rows.Scan(&r.ID, &r.Name, &raw, &r.Version); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(raw, &r.Rules)
		out = append(out, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	visible := []Role{}
	for _, r := range out {
		if s.roleVisible(ctx, s.Pool, a, r.ID) {
			visible = append(visible, r)
		}
	}
	return visible, nil
}
func validateRole(in Role) error {
	if !ValidText(in.Name, 100) || len(in.Rules) > 100 {
		return Bad("A role name and at most 100 rules are required.")
	}
	for _, r := range in.Rules {
		if r.Conditions != nil {
			if r.Action != "ask" && r.Action != "decide" {
				return Bad("Action conditions apply only to ask and decide rules.")
			}
			if len(r.Conditions.Resource) > 200 || len(r.Conditions.ResourceVersion) > 200 {
				return Bad("Resource constraints exceed their limits.")
			}
			if r.Conditions.MaxAmountMinor != nil && (*r.Conditions.MaxAmountMinor < 0 || r.Conditions.Currency == "" || *r.Conditions.MaxAmountMinor > 1_000_000_000_000) {
				return Bad("Amount ceiling requires a currency and 0 to 10^12 minor units.")
			}
		}
		if !strings.Contains("|view|message|ask|decide|manage|*|", "|"+r.Action+"|") || r.TargetKind == "" || r.TargetID == "" {
			return Bad("Invalid permission rule.")
		}
	}
	return nil
}
func (s *Store) SaveRole(ctx context.Context, a Actor, in Role) (Role, error) {
	if e := validateRole(in); e != nil {
		return in, e
	}
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.mayEditRole(ctx, tx, a, in) {
			return Denied()
		}
		if e := checkRevision(ctx, tx, a.Scope, "roles", in.ID, in.Version); e != nil {
			return e
		}
		if in.ID == "" {
			in.ID = ID("rol")
		}
		tag, e := tx.Exec(ctx, `INSERT INTO roles(id,scope,name,rules) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,rules=EXCLUDED.rules,version=roles.version+1 WHERE roles.scope=EXCLUDED.scope`, in.ID, a.Scope, in.Name, JSON(in.Rules))
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return Denied()
		}
		if e = tx.QueryRow(ctx, `SELECT version FROM roles WHERE scope=$1 AND id=$2`, a.Scope, in.ID).Scan(&in.Version); e != nil {
			return e
		}
		return event(ctx, tx, a, "role.saved", "", a.ID, map[string]any{"id": in.ID})
	})
	return in, e
}
func (s *Store) BindRole(ctx context.Context, a Actor, b Binding, remove bool) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		target, e := s.bindingTarget(ctx, tx, a, b)
		if e != nil {
			return e
		}
		var role string
		if e = tx.QueryRow(ctx, `SELECT id FROM roles WHERE scope=$1 AND id=$2`, a.Scope, b.RoleID).Scan(&role); e != nil {
			return normalize(e)
		}
		if !s.mayAssign(ctx, tx, a, b.RoleID, target) {
			return Denied()
		}
		if remove {
			_, e = tx.Exec(ctx, `DELETE FROM role_bindings WHERE scope=$1 AND role_id=$2 AND subject_kind=$3 AND subject_id=$4`, a.Scope, b.RoleID, b.SubjectKind, b.SubjectID)
		} else {
			_, e = tx.Exec(ctx, `INSERT INTO role_bindings(scope,role_id,subject_kind,subject_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, a.Scope, b.RoleID, b.SubjectKind, b.SubjectID)
		}
		if e != nil {
			return e
		}
		return event(ctx, tx, a, "role.binding_changed", "", a.ID, b)
	})
}
func (s *Store) Bindings(ctx context.Context, a Actor) ([]Binding, error) {
	rows, e := s.Pool.Query(ctx, `SELECT role_id,subject_kind,subject_id FROM role_bindings WHERE scope=$1`, a.Scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Binding{}
	for rows.Next() {
		var b Binding
		if e = rows.Scan(&b.RoleID, &b.SubjectKind, &b.SubjectID); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	visible := []Binding{}
	for _, b := range out {
		if s.bindingVisible(ctx, s.Pool, a, b) {
			visible = append(visible, b)
		}
	}
	return visible, nil
}
func (s *Store) Groups(ctx context.Context, a Actor) ([]Group, error) {
	ctx = withPolicySnapshot(ctx)
	rows, e := s.Pool.Query(ctx, `SELECT g.id,g.name,COALESCE(jsonb_agg(m.principal_id) FILTER(WHERE m.principal_id IS NOT NULL),'[]'),g.version,g.created_by FROM groups g LEFT JOIN memberships m ON m.scope=g.scope AND m.group_id=g.id WHERE g.scope=$1 GROUP BY g.id ORDER BY g.name`, a.Scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	all := []Group{}
	for rows.Next() {
		var g Group
		var raw []byte
		if e = rows.Scan(&g.ID, &g.Name, &raw, &g.Version, &g.CreatedBy); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(raw, &g.Members)
		all = append(all, g)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	visible := []Group{}
	for _, g := range all {
		if a.Admin || s.allowed(ctx, s.Pool, a, "view", Target{"group", g.ID}, "", "") || s.manages(ctx, s.Pool, a, "group", g.ID) || contains(g.Members, a.ID) {
			members := []string{}
			for _, id := range g.Members {
				p, e := principal(ctx, s.Pool, a.Scope, id)
				if e == nil && s.inspectIdentity(ctx, s.Pool, a, p) {
					members = append(members, id)
				}
			}
			g.Member = contains(g.Members, a.ID)
			g.MembersComplete = len(members) == len(g.Members)
			g.Members = members
			visible = append(visible, g)
		}
	}
	return visible, nil
}
func (s *Store) SaveGroup(ctx context.Context, a Actor, g Group) (Group, error) {
	// Omitting members on an existing group is a name-only edit. An explicit
	// empty array still means clear membership, under the usual assignment checks.
	preserveMembers := g.ID != "" && g.Members == nil
	if !ValidText(g.Name, 100) || len(g.Members) > 1000 {
		return g, Bad("A name and at most 1000 members are required.")
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		target := g.ID
		if target == "" {
			target = "*"
		}
		if !s.manages(ctx, tx, a, "group", target) {
			return Denied()
		}
		old := []string{}
		if g.ID != "" {
			rows, e := tx.Query(ctx, `SELECT principal_id FROM memberships WHERE scope=$1 AND group_id=$2`, a.Scope, g.ID)
			if e != nil {
				return e
			}
			for rows.Next() {
				var id string
				if e = rows.Scan(&id); e != nil {
					rows.Close()
					return e
				}
				old = append(old, id)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
		if preserveMembers {
			g.Members = old
		}
		if !s.membershipChangeAllowed(ctx, tx, a, g.ID, old, g.Members) {
			return Denied()
		}
		if e := checkRevision(ctx, tx, a.Scope, "groups", g.ID, g.Version); e != nil {
			return e
		}
		if g.ID == "" {
			g.ID = ID("grp")
			g.CreatedBy = a.ID
		}
		g.CreatedBy = a.ID // Trusted on INSERT; existing provenance is immutable.
		tag, e := tx.Exec(ctx, `INSERT INTO groups(id,scope,name,created_by) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,version=groups.version+1 WHERE groups.scope=EXCLUDED.scope`, g.ID, a.Scope, g.Name, g.CreatedBy)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return Denied()
		}
		if _, e = tx.Exec(ctx, `DELETE FROM memberships WHERE scope=$1 AND group_id=$2`, a.Scope, g.ID); e != nil {
			return e
		}
		for _, id := range g.Members {
			if _, e = tx.Exec(ctx, `INSERT INTO memberships(scope,group_id,principal_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, a.Scope, g.ID, id); e != nil {
				return e
			}
		}
		if e = tx.QueryRow(ctx, `SELECT version,created_by FROM groups WHERE scope=$1 AND id=$2`, a.Scope, g.ID).Scan(&g.Version, &g.CreatedBy); e != nil {
			return e
		}
		g.Member = contains(g.Members, a.ID)
		g.MembersComplete = true
		return event(ctx, tx, a, "group.saved", "", a.ID, map[string]any{"id": g.ID})
	})
	g.Member = contains(g.Members, a.ID)
	g.MembersComplete = true
	if err == nil && preserveMembers {
		// A name-only edit must not reveal the hidden roster preserved above.
		visible, e := s.queryRecord(ctx, a, "groups", g.ID)
		if e != nil {
			return Group{}, e
		}
		return visible.(Group), nil
	}
	return g, err
}

// ExplainSubject deliberately simulates role grants, not an administrator credential a person might possess.
func (s *Store) ExplainSubject(ctx context.Context, caller Actor, subject, action string, target Target, connection, actionName string, descriptor ...*Action) (map[string]any, error) {
	a := caller
	if subject != "" {
		p, e := principal(ctx, s.Pool, caller.Scope, subject)
		if e != nil || p.Disabled || !s.inspectAccess(ctx, s.Pool, caller, p) {
			return nil, Missing()
		}
		a = Actor{Principal: p, Scope: caller.Scope, CredentialKind: "api"}
	}
	result, e := s.Explain(ctx, a, action, target, connection, actionName, descriptor...)
	if e != nil {
		return nil, e
	}
	access, e := s.Access(ctx, caller, a.ID)
	if e != nil {
		return nil, e
	}
	matches := []map[string]any{}
	for _, assignment := range access.Roles {
		for index, rule := range assignment.Role.Rules {
			if s.ruleAllows(ctx, s.Pool, a, rule, action, target, connection, actionName, descriptor...) {
				matches = append(matches, map[string]any{"role_id": assignment.Role.ID, "subject_kind": assignment.SubjectKind, "subject_id": assignment.SubjectID, "rule_index": index})
			}
		}
	}
	result["matched_grants"] = matches
	result["permission_only"] = true
	if action == "manage" && target.Kind == "group" && s.manages(ctx, s.Pool, a, "group", target.ID) && !s.allowed(ctx, s.Pool, a, "manage", target, "", "") {
		result["derived_authority"] = "created_group_with_current_managed_members"
	}
	result["subject_id"] = a.ID
	result["basis"] = "effective_role_grants"
	if subject == "" && a.Admin {
		result["basis"] = "administrator_credential"
	}
	return result, nil
}

func conditionsAllow(c *RuleConditions, action *Action) bool {
	if c == nil {
		return true
	}
	if c.Before != nil && !time.Now().Before(*c.Before) {
		return false
	}
	if c.Resource == "" && c.ResourceVersion == "" && c.Currency == "" && c.MaxAmountMinor == nil {
		return true
	}
	if action == nil {
		return false
	}
	if c.Resource != "" && c.Resource != action.Resource {
		return false
	}
	if c.ResourceVersion != "" && c.ResourceVersion != action.ResourceVersion {
		return false
	}
	if c.Currency != "" && action.Arguments["currency"] != c.Currency {
		return false
	}
	if c.MaxAmountMinor != nil {
		var amount int64
		switch n := action.Arguments["amount_minor"].(type) {
		case int:
			amount = int64(n)
		case int64:
			amount = n
		case float64:
			if n < 0 || n > 1e12 || n != math.Trunc(n) {
				return false
			}
			amount = int64(n)
		default:
			return false
		}
		if amount < 0 || amount > *c.MaxAmountMinor {
			return false
		}
	}
	return true
}
