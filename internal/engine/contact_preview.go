package engine

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/jackc/pgx/v5"
)

type ContactPreviewInput struct {
	Subject Target   `json:"subject"`
	Rules   *[]Rule  `json:"rules,omitempty"`
	Version int64    `json:"version"`
	Targets []Target `json:"targets"`
}
type ContactPermissionCell struct {
	Allowed bool     `json:"allowed"`
	Blocked bool     `json:"blocked"`
	Limited bool     `json:"limited"`
	Direct  string   `json:"direct"`
	Sources []string `json:"sources"`
}
type ContactPermissionRow struct {
	Target      Target                           `json:"target"`
	Name        string                           `json:"name"`
	Permissions map[string]ContactPermissionCell `json:"permissions"`
}
type ContactPreview struct {
	Policy   Role                   `json:"policy"`
	Contacts []ContactPermissionRow `json:"contacts"`
	Warnings []string               `json:"warnings"`
	CanSave  bool                   `json:"can_save"`
}

func contactPolicyID(scope string, target Target) string {
	return "rol_contacts_" + digestJSON([]any{scope, target})[:24]
}

// A bounded editing-time observation, never a grant or a dispatch. Sources and
// group membership are evaluated by the same matcher used by live requests.
func (s *Store) PreviewContactPermissions(ctx context.Context, a Actor, in ContactPreviewInput) (ContactPreview, error) {
	out := ContactPreview{Contacts: []ContactPermissionRow{}, Warnings: []string{}}
	if len(in.Targets) > 100 {
		return out, Bad("Choose at most 100 contacts.")
	}
	err := s.tx(ctx, a, func(db pgx.Tx) error {
		ctx = withPolicySnapshot(ctx)
		b := Binding{SubjectKind: "principal", SubjectID: in.Subject.ID}
		if in.Subject.Kind == "group" {
			b.SubjectKind = "group"
		}
		target, err := s.bindingTarget(ctx, db, a, b)
		if err != nil || target != in.Subject {
			return Missing()
		}
		subject := Actor{Scope: a.Scope, Principal: Principal{ID: target.ID, Kind: target.Kind}}
		if target.Kind == "group" {
			if !a.Admin && !s.manages(ctx, db, a, "group", target.ID) {
				return Missing()
			}
		} else {
			p, err := principal(ctx, db, a.Scope, target.ID)
			if err != nil || !s.inspectAccess(ctx, db, a, p) {
				return Missing()
			}
			subject.Principal = p
		}
		rows, err := db.Query(ctx, `SELECT r.id,r.name,r.rules,r.version,b.subject_kind,b.subject_id FROM role_bindings b JOIN roles r ON r.scope=b.scope AND r.id=b.role_id WHERE b.scope=$1 AND ((b.subject_id=$2 AND b.subject_kind=$3) OR ($3='principal' AND b.subject_kind='group' AND EXISTS(SELECT 1 FROM memberships m WHERE m.scope=b.scope AND m.group_id=b.subject_id AND m.principal_id=$2))) ORDER BY r.id,b.subject_kind,b.subject_id`, a.Scope, target.ID, b.SubjectKind)
		if err != nil {
			return err
		}
		assignments := []EffectiveRole{}
		for rows.Next() {
			var x EffectiveRole
			var raw []byte
			if err = rows.Scan(&x.Role.ID, &x.Role.Name, &raw, &x.Role.Version, &x.SubjectKind, &x.SubjectID); err != nil {
				rows.Close()
				return err
			}
			if err = json.Unmarshal(raw, &x.Role.Rules); err != nil {
				rows.Close()
				return err
			}
			assignments = append(assignments, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		id := contactPolicyID(a.Scope, in.Subject)
		out.Policy = Role{ID: id, Name: "Contacts " + target.ID, Rules: []Rule{}}
		effective := []EffectiveRole{}
		for _, x := range assignments {
			if x.Role.ID == id && x.SubjectKind == b.SubjectKind && x.SubjectID == target.ID {
				out.Policy = x.Role
			} else {
				effective = append(effective, x)
			}
		}
		if out.Policy.Version == 0 {
			// An administrator may have unassigned the dedicated policy without
			// deleting it. Keep its revision, but do not restore its old grants.
			err = db.QueryRow(ctx, `SELECT version FROM roles WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&out.Policy.Version)
			if err != nil && err != pgx.ErrNoRows {
				return err
			}
		}
		draft := out.Policy
		if in.Rules != nil {
			if in.Version != draft.Version {
				return Conflict("Permissions changed. Reload before saving.")
			}
			draft.Rules = *in.Rules
			if err = validateRole(draft); err != nil {
				return err
			}
			if err = validateContactRules(draft.Rules); err != nil {
				return err
			}
		}
		check := draft
		if check.Version == 0 {
			check.ID = ""
		}
		out.CanSave = s.mayEditRole(ctx, db, a, check) && s.mayAssign(ctx, db, a, id, target)
		if in.Rules != nil && !out.CanSave {
			return Denied()
		}
		effective = append(effective, EffectiveRole{Role: draft, SubjectKind: b.SubjectKind, SubjectID: target.ID})
		names := map[string]string{}
		for _, x := range effective {
			if x.SubjectKind == "group" && x.SubjectID != target.ID {
				names[x.SubjectID] = "Group policy"
				if a.Admin || s.manages(ctx, db, a, "group", x.SubjectID) || s.allowed(ctx, db, a, "view", Target{"group", x.SubjectID}, "", "") {
					var name string
					if db.QueryRow(ctx, `SELECT name FROM groups WHERE scope=$1 AND id=$2`, a.Scope, x.SubjectID).Scan(&name) == nil {
						names[x.SubjectID] = "Via " + name
					}
				}
			}
		}
		targets := append([]Target{}, in.Targets...)
		if in.Rules == nil {
			for _, x := range effective {
				for _, r := range x.Role.Rules {
					if (r.Action == "view" || r.Action == "message" || r.Action == "ask" || r.Action == "decide" || r.Action == "*") && r.TargetID != "*" && r.TargetID != "$self" && (r.TargetKind == "person" || r.TargetKind == "agent" || r.TargetKind == "service" || r.TargetKind == "group") {
						targets = append(targets, Target{r.TargetKind, r.TargetID})
					}
				}
			}
		}
		seen := map[Target]bool{}
		for _, t := range targets {
			if seen[t] || len(out.Contacts) >= 100 {
				continue
			}
			seen[t] = true
			if err = t.Validate(); err != nil {
				return err
			}
			if t.Kind == "instance" {
				return Bad("Choose a logical contact, not a connection.")
			}
			name := "Restricted contact"
			if t.Kind == "group" {
				if a.Admin || s.manages(ctx, db, a, "group", t.ID) || s.allowed(ctx, db, a, "view", t, "", "") {
					if err = db.QueryRow(ctx, `SELECT name FROM groups WHERE scope=$1 AND id=$2`, a.Scope, t.ID).Scan(&name); err != nil {
						return Missing()
					}
				}
			} else {
				p, err := principal(ctx, db, a.Scope, t.ID)
				if err != nil || p.Kind != t.Kind {
					return Missing()
				}
				if s.inspectIdentity(ctx, db, a, p) {
					name = p.Name
				}
			}
			row := ContactPermissionRow{Target: t, Name: name, Permissions: map[string]ContactPermissionCell{}}
			for _, action := range []string{"view", "message", "ask", "decide"} {
				cell := ContactPermissionCell{Direct: "default", Sources: []string{}}
				allows, blocks := []string{}, []string{}
				for _, x := range effective {
					for _, r := range x.Role.Rules {
						selector := r
						selector.ConnectionID = ""
						selector.ActionName = ""
						selector.Conditions = nil
						if !s.ruleMatches(ctx, db, subject, selector, action, t, "", "") {
							continue
						}
						direct := x.Role.ID == id && x.SubjectID == target.ID && x.SubjectKind == b.SubjectKind
						source := x.Role.Name
						if direct && r.TargetKind == t.Kind && r.TargetID == t.ID {
							source = "Allowed directly"
						} else if r.TargetKind == "group" && t.Kind != "group" {
							source = "Via group"
							if a.Admin || s.manages(ctx, db, a, "group", r.TargetID) {
								var name string
								if db.QueryRow(ctx, `SELECT name FROM groups WHERE scope=$1 AND id=$2`, a.Scope, r.TargetID).Scan(&name) == nil {
									source = "Via " + name
								}
							}
						} else if x.SubjectKind == "group" {
							source = names[x.SubjectID]
						}
						if direct && r.TargetKind == t.Kind && r.TargetID == t.ID && r.Action == action {
							cell.Direct = "allow"
							if r.Effect == "deny" {
								cell.Direct = "deny"
							}
						}
						if r.Effect == "deny" {
							if direct {
								source = "Blocked directly"
							}
							blocks = append(blocks, source)
						} else if r.ConnectionID == "" && r.ActionName == "" && r.Conditions == nil {
							allows = append(allows, source)
						} else {
							cell.Limited = true
						}
					}
				}
				cell.Blocked = len(blocks) > 0
				cell.Allowed = len(allows) > 0 && !cell.Blocked
				if cell.Blocked {
					cell.Sources = distinctIDs(blocks)
				} else {
					cell.Sources = distinctIDs(allows)
				}
				row.Permissions[action] = cell
			}
			if !row.Permissions["view"].Allowed && (row.Permissions["message"].Allowed || row.Permissions["ask"].Allowed || row.Permissions["decide"].Allowed) {
				out.Warnings = append(out.Warnings, name+": communication also needs visibility to read the conversation.")
			}
			if in.Rules != nil && row.Permissions["ask"].Allowed && t.Kind != "group" && target.Kind != "group" {
				p, err := principal(ctx, db, a.Scope, t.ID)
				if err == nil && !s.requestRecipientCanRespond(ctx, db, subject, p, "question", nil) {
					out.Warnings = append(out.Warnings, name+" cannot currently reply to this contact. Check their visibility or message blocks.")
				}
			}
			out.Contacts = append(out.Contacts, row)
		}
		sort.SliceStable(out.Contacts, func(i, j int) bool { return out.Contacts[i].Name < out.Contacts[j].Name })
		return nil
	})
	return out, err
}

func validateContactRules(rules []Rule) error {
	for _, r := range rules {
		if r.Action != "view" && r.Action != "message" && r.Action != "ask" && r.Action != "decide" {
			return Bad("Only communication permissions can be edited here.")
		}
		if r.TargetID == "*" || r.TargetID == "$self" || r.ConnectionID != "" || r.ActionName != "" || r.Conditions != nil || (r.TargetKind != "person" && r.TargetKind != "agent" && r.TargetKind != "service" && r.TargetKind != "group") {
			return Bad("Choose explicit contacts for this policy.")
		}
	}
	return nil
}
