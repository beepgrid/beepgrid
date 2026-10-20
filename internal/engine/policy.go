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
	rows, e := db.Query(ctx, `SELECT r.rules FROM roles r JOIN role_bindings b ON b.role_id=r.id AND b.scope=r.scope WHERE r.scope=$1 AND ((b.subject_kind='principal' AND b.subject_id=$2) OR (b.subject_kind='group' AND EXISTS(SELECT 1 FROM memberships m WHERE m.scope=b.scope AND m.group_id=b.subject_id AND m.principal_id=$2)))`, a.Scope, a.ID)
	if e != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			return false
		}
		var rules []Rule
		if json.Unmarshal(raw, &rules) != nil {
			return false
		}
		for _, r := range rules {
			if r.Action != action && r.Action != "*" {
				continue
			}
			if r.TargetKind != "*" && r.TargetKind != target.Kind {
				continue
			}
			if r.TargetID != "*" && r.TargetID != target.ID {
				continue
			}
			if r.ConnectionID != "" && r.ConnectionID != connection && connection != "*" {
				continue
			}
			if r.ActionName != "" && r.ActionName != actionName {
				continue
			}
			var actionDetail *Action
			if len(descriptor) > 0 {
				actionDetail = descriptor[0]
			}
			if !conditionsAllow(r.Conditions, actionDetail) {
				continue
			}
			return true
		}
	}
	return false
}
func (s *Store) Explain(ctx context.Context, a Actor, action string, target Target, connection, actionName string, descriptor ...*Action) (map[string]any, error) {
	if a.CredentialKind == "enrollment" {
		return nil, Denied()
	}
	if err := target.Validate(); err != nil {
		return nil, err
	}
	if target.Kind != "group" && target.Kind != "instance" {
		p, e := principal(ctx, s.Pool, a.Scope, target.ID)
		if e != nil || p.Disabled {
			return map[string]any{"allowed": false, "reason": "Target unavailable or not permitted."}, nil
		}
	}
	allowed := s.allowed(ctx, s.Pool, a, action, target, connection, actionName, descriptor...)
	return map[string]any{"allowed": allowed, "action": action, "target": target, "reason": map[bool]string{true: "An effective role or administrator credential permits this operation.", false: "No effective permission permits this operation."}[allowed]}, nil
}
func (s *Store) Roles(ctx context.Context, a Actor) ([]Role, error) {
	if !a.Admin && !s.allowed(ctx, s.Pool, a, "manage", Target{"role", "*"}, "", "") {
		return nil, Denied()
	}
	rows, e := s.Pool.Query(ctx, `SELECT id,name,rules FROM roles WHERE scope=$1 ORDER BY name`, a.Scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		var r Role
		var raw []byte
		if e = rows.Scan(&r.ID, &r.Name, &raw); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(raw, &r.Rules)
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) SaveRole(ctx context.Context, a Actor, in Role) (Role, error) {
	if !ValidText(in.Name, 100) || len(in.Rules) > 100 {
		return in, Bad("A role name and at most 100 rules are required.")
	}
	for _, r := range in.Rules {
		if r.Conditions != nil {
			if r.Action != "ask" && r.Action != "decide" {
				return in, Bad("Action conditions apply only to ask and decide rules.")
			}
			if len(r.Conditions.Resource) > 200 || len(r.Conditions.ResourceVersion) > 200 {
				return in, Bad("Resource constraints exceed their limits.")
			}
			if r.Conditions.MaxAmountMinor != nil && (*r.Conditions.MaxAmountMinor < 0 || r.Conditions.Currency == "" || *r.Conditions.MaxAmountMinor > 1_000_000_000_000) {
				return in, Bad("Amount ceiling requires a currency and 0 to 10^12 minor units.")
			}
		}
		if !strings.Contains("|view|message|ask|decide|manage|*|", "|"+r.Action+"|") || r.TargetKind == "" || r.TargetID == "" {
			return in, Bad("Invalid permission rule.")
		}
	}
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !a.Admin {
			return Denied()
		}
		if in.ID == "" {
			in.ID = ID("rol")
		}
		tag, e := tx.Exec(ctx, `INSERT INTO roles(id,scope,name,rules) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,rules=EXCLUDED.rules WHERE roles.scope=EXCLUDED.scope`, in.ID, a.Scope, in.Name, JSON(in.Rules))
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return Denied()
		}
		return event(ctx, tx, a, "role.saved", "", a.ID, map[string]any{"id": in.ID})
	})
	return in, e
}
func (s *Store) BindRole(ctx context.Context, a Actor, b Binding, remove bool) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		if !a.Admin {
			return Denied()
		}
		if b.SubjectKind == "principal" {
			if _, e := principal(ctx, tx, a.Scope, b.SubjectID); e != nil {
				return e
			}
		} else if b.SubjectKind == "group" {
			var id string
			if e := tx.QueryRow(ctx, `SELECT id FROM groups WHERE scope=$1 AND id=$2`, a.Scope, b.SubjectID).Scan(&id); e != nil {
				return e
			}
		} else {
			return Bad("Invalid role subject.")
		}
		var e error
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
	if !a.Admin {
		return nil, Denied()
	}
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
	return out, rows.Err()
}
func (s *Store) Groups(ctx context.Context, a Actor) ([]Group, error) {
	rows, e := s.Pool.Query(ctx, `SELECT g.id,g.name,COALESCE(jsonb_agg(m.principal_id) FILTER(WHERE m.principal_id IS NOT NULL),'[]') FROM groups g LEFT JOIN memberships m ON m.scope=g.scope AND m.group_id=g.id WHERE g.scope=$1 GROUP BY g.id ORDER BY g.name`, a.Scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	all := []Group{}
	for rows.Next() {
		var g Group
		var raw []byte
		if e = rows.Scan(&g.ID, &g.Name, &raw); e != nil {
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
		if a.Admin || s.allowed(ctx, s.Pool, a, "view", Target{"group", g.ID}, "", "") {
			visible = append(visible, g)
		}
	}
	return visible, nil
}
func (s *Store) SaveGroup(ctx context.Context, a Actor, g Group) (Group, error) {
	if !ValidText(g.Name, 100) || len(g.Members) > 1000 {
		return g, Bad("A name and at most 1000 members are required.")
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !a.Admin {
			return Denied()
		}
		if g.ID == "" {
			g.ID = ID("grp")
		}
		tag, e := tx.Exec(ctx, `INSERT INTO groups(id,scope,name) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name WHERE groups.scope=EXCLUDED.scope`, g.ID, a.Scope, g.Name)
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
		return event(ctx, tx, a, "group.saved", "", a.ID, map[string]any{"id": g.ID})
	})
	return g, err
}

// ExplainSubject deliberately simulates role grants, not an administrator credential a person might possess.
func (s *Store) ExplainSubject(ctx context.Context, caller Actor, subject, action string, target Target, connection, actionName string, descriptor ...*Action) (map[string]any, error) {
	a := caller
	if subject != "" {
		if !caller.Admin && subject != caller.ID {
			return nil, Denied()
		}
		p, e := principal(ctx, s.Pool, caller.Scope, subject)
		if e != nil || p.Disabled {
			return nil, Missing()
		}
		a = Actor{Principal: p, Scope: caller.Scope, CredentialKind: "api"}
	}
	result, e := s.Explain(ctx, a, action, target, connection, actionName, descriptor...)
	if e != nil {
		return nil, e
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
