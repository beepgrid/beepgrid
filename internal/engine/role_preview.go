package engine

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type PolicyCheck struct {
	SubjectID    string  `json:"subject_id"`
	Action       string  `json:"action"`
	Target       Target  `json:"target"`
	ConnectionID string  `json:"connection_id,omitempty"`
	ActionName   string  `json:"action_name,omitempty"`
	Descriptor   *Action `json:"descriptor,omitempty"`
}
type RolePreviewInput struct {
	Role   Role          `json:"role"`
	Checks []PolicyCheck `json:"checks"`
}
type PolicyComparison struct {
	Check  PolicyCheck `json:"check"`
	Before bool        `json:"before"`
	After  bool        `json:"after"`
}
type RolePreview struct {
	CanSave  bool               `json:"can_save"`
	Version  int64              `json:"version"`
	Affected []Binding          `json:"affected_assignments"`
	Checks   []PolicyComparison `json:"checks"`
	Basis    string             `json:"basis"`
	Saves    bool               `json:"saves"`
}

// Simulate only this role's existing assignments. This never grants a role to a
// new subject or includes the administrator credential bypass in a simulation.
func (s *Store) PreviewRole(ctx context.Context, a Actor, in RolePreviewInput) (RolePreview, error) {
	out := RolePreview{Version: in.Role.Version, Affected: []Binding{}, Checks: []PolicyComparison{}, Basis: "effective_role_grants"}
	if e := validateRole(in.Role); e != nil {
		return out, e
	}
	if len(in.Checks) > 100 {
		return out, Bad("At most 100 policy checks are permitted.")
	}
	e := s.tx(ctx, a, func(db pgx.Tx) error {
		ctx := withPolicySnapshot(ctx)
		if !s.mayEditRole(ctx, db, a, in.Role) {
			return Denied()
		}
		if e := checkRevision(ctx, db, a.Scope, "roles", in.Role.ID, in.Role.Version); e != nil {
			return e
		}
		if in.Role.ID != "" {
			if e := db.QueryRow(ctx, `SELECT version FROM roles WHERE scope=$1 AND id=$2`, a.Scope, in.Role.ID).Scan(&out.Version); e != nil {
				return normalize(e)
			}
			rows, e := db.Query(ctx, `SELECT role_id,subject_kind,subject_id FROM role_bindings WHERE scope=$1 AND role_id=$2 ORDER BY subject_kind,subject_id`, a.Scope, in.Role.ID)
			if e != nil {
				return e
			}
			for rows.Next() {
				var b Binding
				if e = rows.Scan(&b.RoleID, &b.SubjectKind, &b.SubjectID); e != nil {
					rows.Close()
					return e
				}
				out.Affected = append(out.Affected, b)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
		type preparedSubject struct {
			person        Principal
			before, after []Rule
		}
		subjects := map[string]preparedSubject{}
		for _, check := range in.Checks {
			switch check.Action {
			case "view", "message", "ask", "decide", "manage":
			default:
				return Bad("Unsupported permission action.")
			}
			if e := check.Target.Validate(); e != nil {
				if check.Action != "manage" {
					return e
				}
				if _, ok := PermissionCatalog()["management_targets"].(map[string]string)[check.Target.Kind]; !ok || !ValidText(check.Target.ID, 256) {
					return e
				}
			}
			prepared, ok := subjects[check.SubjectID]
			if !ok {
				p, e := principal(ctx, db, a.Scope, check.SubjectID)
				if e != nil {
					return e
				}
				if !s.inspectAccess(ctx, db, a, p) && !s.mayAssign(ctx, db, a, in.Role.ID, Target{p.Kind, p.ID}) {
					return Denied()
				}
				subject := Actor{Principal: p, Scope: a.Scope}
				before, e := roleRules(ctx, db, subject)
				if e != nil {
					return e
				}
				after := []Rule{}
				// Read the other roles independently so a duplicate matching rule is not
				// accidentally subtracted when only this role changes.
				rows, e := db.Query(ctx, `SELECT DISTINCT r.id,r.rules FROM roles r JOIN role_bindings b ON b.scope=r.scope AND b.role_id=r.id WHERE r.scope=$1 AND r.id<>$3 AND ((b.subject_kind='principal' AND b.subject_id=$2) OR (b.subject_kind='group' AND EXISTS(SELECT 1 FROM memberships m WHERE m.scope=b.scope AND m.group_id=b.subject_id AND m.principal_id=$2)))`, a.Scope, p.ID, in.Role.ID)
				if e != nil {
					return e
				}
				for rows.Next() {
					var id string
					var raw []byte
					if e = rows.Scan(&id, &raw); e != nil {
						rows.Close()
						return e
					}
					rules, e := decodeRules(raw)
					if e != nil {
						rows.Close()
						return e
					}
					after = append(after, rules...)
				}
				e = rows.Err()
				rows.Close()
				if e != nil {
					return e
				}
				if in.Role.ID != "" && s.roleAssignedTo(ctx, db, subject, in.Role.ID) {
					after = append(after, in.Role.Rules...)
				}
				prepared = preparedSubject{p, before, after}
				subjects[check.SubjectID] = prepared
			}
			p := prepared.person
			subject := Actor{Principal: p, Scope: a.Scope}
			before, after := prepared.before, prepared.after
			comparison := PolicyComparison{Check: check}
			if !p.Disabled {
				for _, r := range before {
					if s.ruleAllows(ctx, db, subject, r, check.Action, check.Target, check.ConnectionID, check.ActionName, check.Descriptor) {
						comparison.Before = true
					}
				}
				for _, r := range after {
					if s.ruleAllows(ctx, db, subject, r, check.Action, check.Target, check.ConnectionID, check.ActionName, check.Descriptor) {
						comparison.After = true
					}
				}
			}
			out.Checks = append(out.Checks, comparison)
		}
		out.CanSave = true
		return nil
	})
	return out, e
}
