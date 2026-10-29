package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

type ContactPermissionsInput struct {
	Subject Target `json:"subject"`
	Rules   []Rule `json:"rules"`
	Version int64  `json:"version"`
}

// SaveContactPermissions edits only a dedicated contact policy. Inherited and
// separately managed policies remain explicit sources of effective access.
func (s *Store) SaveContactPermissions(ctx context.Context, a Actor, in ContactPermissionsInput) (Role, error) {
	var out Role
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		subject := in.Subject
		b := Binding{SubjectKind: "principal", SubjectID: subject.ID}
		if subject.Kind == "group" {
			b.SubjectKind = "group"
		}
		target, e := s.bindingTarget(ctx, tx, a, b)
		if e != nil {
			return e
		}
		if target.Kind != subject.Kind {
			return Bad("Subject type does not match.")
		}
		id := "rol_contacts_" + digestJSON([]any{a.Scope, subject})[:24]
		out = Role{ID: id, Name: "Contacts " + subject.ID, Rules: in.Rules, Version: in.Version}
		for _, rule := range in.Rules {
			if rule.Action != "view" && rule.Action != "message" && rule.Action != "ask" && rule.Action != "decide" {
				return Bad("Only communication permissions can be edited here.")
			}
			if rule.TargetID == "*" || rule.TargetID == "$self" || rule.ConnectionID != "" || rule.ActionName != "" || rule.Conditions != nil {
				return Bad("Choose explicit contacts for this policy.")
			}
			if rule.TargetKind == "group" {
				var exists bool
				if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE scope=$1 AND id=$2)`, a.Scope, rule.TargetID).Scan(&exists); e != nil {
					return e
				}
				if !exists {
					return Missing()
				}
			} else {
				p, e := principal(ctx, tx, a.Scope, rule.TargetID)
				if e != nil || p.Kind != rule.TargetKind {
					return Missing()
				}
			}
		}
		if e = validateRole(out); e != nil {
			return e
		}
		var current int64
		e = tx.QueryRow(ctx, `SELECT version FROM roles WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&current)
		check := out
		if e == pgx.ErrNoRows {
			check.ID = ""
		} else if e != nil {
			return e
		}
		if current != in.Version {
			return Conflict("Permissions changed. Reload before saving.")
		}
		if !s.mayEditRole(ctx, tx, a, check) {
			return Denied()
		}
		if _, e = tx.Exec(ctx, `INSERT INTO roles(id,scope,name,rules) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET rules=EXCLUDED.rules,version=roles.version+1 WHERE roles.scope=EXCLUDED.scope`, id, a.Scope, out.Name, JSON(in.Rules)); e != nil {
			return e
		}
		b.RoleID = id
		if !s.mayAssign(ctx, tx, a, id, target) {
			return Denied()
		}
		if _, e = tx.Exec(ctx, `INSERT INTO role_bindings(scope,role_id,subject_kind,subject_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, a.Scope, id, b.SubjectKind, b.SubjectID); e != nil {
			return e
		}
		var raw []byte
		if e = tx.QueryRow(ctx, `SELECT version,rules FROM roles WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&out.Version, &raw); e != nil {
			return e
		}
		if e = json.Unmarshal(raw, &out.Rules); e != nil {
			return e
		}
		return event(ctx, tx, a, "role.binding_changed", "", a.ID, b)
	})
	return out, err
}
