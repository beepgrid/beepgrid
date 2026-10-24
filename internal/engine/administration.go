package engine

import (
	"context"
	"encoding/json"
	"time"
)

// Administrative resources use the same explicit role rules as communication.
// Authority to edit a subject is separate from authority to assign a role.
func (s *Store) manages(ctx context.Context, db DB, a Actor, kind, id string) bool {
	if s.allowed(ctx, db, a, "manage", Target{kind, id}, "", "") {
		return true
	}
	if kind != "group" || !s.allowed(ctx, db, a, "manage", Target{"group_creation", "workspace"}, "", "") {
		return false
	}
	if id == "*" {
		return true
	}
	return s.createdGroupManager(ctx, db, a, id)
}

// Creation authority is bound to the creator and current managed membership.
// It never grants access to an existing group or an expanded outside team.
func (s *Store) createdGroupManager(ctx context.Context, db DB, a Actor, id string) bool {
	var creator string
	if e := db.QueryRow(ctx, `SELECT created_by FROM groups WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&creator); e != nil || creator != a.ID {
		return false
	}
	rows, e := db.Query(ctx, `SELECT principal_id FROM memberships WHERE scope=$1 AND group_id=$2`, a.Scope, id)
	if e != nil {
		return false
	}
	ids := []string{}
	for rows.Next() {
		var pid string
		if rows.Scan(&pid) != nil {
			rows.Close()
			return false
		}
		ids = append(ids, pid)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return false
	}
	for _, pid := range ids {
		p, e := principal(ctx, db, a.Scope, pid)
		if e != nil || !s.managesIdentity(ctx, db, a, p) {
			return false
		}
	}
	return true
}
func (s *Store) inspectIdentity(ctx context.Context, db DB, a Actor, p Principal) bool {
	return p.ID == a.ID || s.allowed(ctx, db, a, "view", Target{p.Kind, p.ID}, "", "") || s.manages(ctx, db, a, p.Kind, p.ID)
}
func (s *Store) protectedIdentity(ctx context.Context, db DB, a Actor, id string) bool {
	if a.Admin {
		return false
	}
	var protected bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, a.Scope, id).Scan(&protected)
	return err != nil || protected
}
func (s *Store) managesIdentity(ctx context.Context, db DB, a Actor, p Principal) bool {
	return s.manages(ctx, db, a, p.Kind, p.ID) && !s.protectedIdentity(ctx, db, a, p.ID)
}
func (s *Store) bindingTarget(ctx context.Context, db DB, a Actor, b Binding) (Target, error) {
	if b.SubjectKind == "principal" {
		p, e := principal(ctx, db, a.Scope, b.SubjectID)
		return Target{p.Kind, p.ID}, e
	}
	if b.SubjectKind != "group" {
		return Target{}, Bad("Invalid role subject.")
	}
	var id string
	e := db.QueryRow(ctx, `SELECT id FROM groups WHERE scope=$1 AND id=$2`, a.Scope, b.SubjectID).Scan(&id)
	return Target{"group", id}, normalize(e)
}
func (s *Store) mayAssign(ctx context.Context, db DB, a Actor, role string, target Target) bool {
	if a.Admin {
		return true
	}
	return s.manages(ctx, db, a, "role_binding", role) && s.manages(ctx, db, a, target.Kind, target.ID) && (target.Kind == "group" || !s.protectedIdentity(ctx, db, a, target.ID))
}
func (s *Store) roleAssignedTo(ctx context.Context, db DB, a Actor, role string) bool {
	var yes bool
	e := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM role_bindings b WHERE b.scope=$1 AND b.role_id=$2 AND ((b.subject_kind='principal' AND b.subject_id=$3) OR (b.subject_kind='group' AND EXISTS(SELECT 1 FROM memberships m WHERE m.scope=b.scope AND m.group_id=b.subject_id AND m.principal_id=$3))))`, a.Scope, role, a.ID).Scan(&yes)
	return e == nil && yes
}
func (s *Store) roleVisible(ctx context.Context, db DB, a Actor, id string) bool {
	return a.Admin || s.manages(ctx, db, a, "role", id) || s.manages(ctx, db, a, "role_binding", id) || s.allowed(ctx, db, a, "view", Target{"role", id}, "", "") || s.roleAssignedTo(ctx, db, a, id)
}
func (s *Store) bindingVisible(ctx context.Context, db DB, a Actor, b Binding) bool {
	if a.Admin || (b.SubjectKind == "principal" && b.SubjectID == a.ID) {
		return true
	}
	target, e := s.bindingTarget(ctx, db, a, b)
	if e != nil {
		return false
	}
	if s.mayAssign(ctx, db, a, b.RoleID, target) {
		return true
	}
	if b.SubjectKind == "group" {
		return targetGroupMember(ctx, db, a.Scope, b.SubjectID, a.ID)
	}
	return false
}

// A role editor may narrow its current grants, never manufacture a broader rule.
// Group selectors are compared symbolically; today's membership is not a ceiling
// for a permanent rule that would outlive a later membership change.
func ruleContains(parent, child Rule, actorID, subjectID string) bool {
	if parent.Action != "*" && parent.Action != child.Action {
		return false
	}
	if parent.TargetKind != "*" && parent.TargetKind != child.TargetKind {
		return false
	}
	parentID, childID := parent.TargetID, child.TargetID
	if parentID == "$self" {
		parentID = actorID
	}
	if childID == "$self" {
		if subjectID == "" && parentID != "*" {
			return false
		}
		if subjectID != "" {
			childID = subjectID
		}
	}
	if parentID != "*" && parentID != childID {
		return false
	}
	if parent.ConnectionID != "" && parent.ConnectionID != child.ConnectionID {
		return false
	}
	if parent.ActionName != "" && parent.ActionName != child.ActionName {
		return false
	}
	p, c := parent.Conditions, child.Conditions
	if p == nil {
		return true
	}
	if c == nil {
		return false
	}
	if p.Before != nil && (c.Before == nil || c.Before.After(*p.Before)) {
		return false
	}
	if p.Resource != "" && p.Resource != c.Resource {
		return false
	}
	if p.ResourceVersion != "" && p.ResourceVersion != c.ResourceVersion {
		return false
	}
	if p.Currency != "" && p.Currency != c.Currency {
		return false
	}
	return p.MaxAmountMinor == nil || (c.MaxAmountMinor != nil && *c.MaxAmountMinor <= *p.MaxAmountMinor)
}
func (s *Store) rulesWithin(ctx context.Context, db DB, a Actor, rules []Rule, subject string) bool {
	if a.Admin {
		return true
	}
	ceiling, e := roleRules(ctx, db, a)
	if e != nil {
		return false
	}
	for _, child := range rules {
		covered := false
		for _, parent := range ceiling {
			if ruleContains(parent, child, a.ID, subject) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}
func (s *Store) mayEditRole(ctx context.Context, db DB, a Actor, r Role) bool {
	target := r.ID
	if target == "" {
		target = "*"
	}
	if !s.manages(ctx, db, a, "role", target) || !s.rulesWithin(ctx, db, a, r.Rules, "") {
		return false
	}
	if a.Admin || r.ID == "" {
		return true
	}
	rows, e := db.Query(ctx, `SELECT role_id,subject_kind,subject_id FROM role_bindings WHERE scope=$1 AND role_id=$2`, a.Scope, r.ID)
	if e != nil {
		return false
	}
	bindings := []Binding{}
	for rows.Next() {
		var b Binding
		if rows.Scan(&b.RoleID, &b.SubjectKind, &b.SubjectID) != nil {
			rows.Close()
			return false
		}
		bindings = append(bindings, b)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return false
	}
	for _, b := range bindings {
		target, e := s.bindingTarget(ctx, db, a, b)
		if e != nil || !s.mayAssign(ctx, db, a, r.ID, target) {
			return false
		}
	}
	return true
}
func (s *Store) membershipChangeAllowed(ctx context.Context, db DB, a Actor, group string, old, next []string) bool {
	if a.Admin {
		return true
	}
	before, after := map[string]bool{}, map[string]bool{}
	for _, id := range old {
		before[id] = true
	}
	for _, id := range next {
		after[id] = true
	}
	roles := []string{}
	rows, e := db.Query(ctx, `SELECT role_id FROM role_bindings WHERE scope=$1 AND subject_kind='group' AND subject_id=$2`, a.Scope, group)
	if e != nil {
		return false
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return false
		}
		roles = append(roles, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return false
	}
	for id := range before {
		if after[id] {
			continue
		}
		after[id] = false
	}
	for id, added := range after {
		if added && before[id] {
			continue
		}
		p, e := principal(ctx, db, a.Scope, id)
		if e != nil || !s.managesIdentity(ctx, db, a, p) {
			return false
		}
		for _, role := range roles {
			if !s.mayAssign(ctx, db, a, role, Target{p.Kind, p.ID}) {
				return false
			}
		}
	}
	return true
}
func (s *Store) mayIssueCredential(ctx context.Context, db DB, a Actor, p Principal) bool {
	if a.Admin {
		return true
	}
	if !s.manages(ctx, db, a, "credential", p.ID) || !s.managesIdentity(ctx, db, a, p) {
		return false
	}
	rules, e := roleRules(ctx, db, Actor{Principal: p, Scope: a.Scope})
	return e == nil && s.rulesWithin(ctx, db, a, rules, p.ID)
}

// A short-lived capability result is explanatory, never a reusable permit.
type ResourceCapabilities struct {
	View       bool      `json:"view"`
	Manage     bool      `json:"manage"`
	Message    bool      `json:"message"`
	Ask        bool      `json:"ask"`
	Decide     bool      `json:"decide"`
	ObservedAt time.Time `json:"observed_at"`
}

func decodeRules(raw []byte) ([]Rule, error) {
	var rules []Rule
	e := json.Unmarshal(raw, &rules)
	return rules, e
}

func (s *Store) mayLinkAccount(ctx context.Context, db DB, a Actor, id string) bool {
	if a.Admin {
		return true
	}
	p, e := principal(ctx, db, a.Scope, id)
	if e != nil || p.Kind != "person" || !s.managesIdentity(ctx, db, a, p) || !s.manages(ctx, db, a, "account", id) {
		return false
	}
	rules, e := roleRules(ctx, db, Actor{Principal: p, Scope: a.Scope})
	return e == nil && s.rulesWithin(ctx, db, a, rules, id)
}
func (s *Store) mayManageDestination(ctx context.Context, db DB, a Actor, connection, id string) bool {
	if a.Admin {
		return true
	}
	p, e := principal(ctx, db, a.Scope, id)
	return e == nil && s.managesIdentity(ctx, db, a, p) && s.manages(ctx, db, a, "connection", connection) && s.manages(ctx, db, a, "destination", id)
}
func (s *Store) mayManageConnection(ctx context.Context, db DB, a Actor, id string) bool {
	target := id
	if target == "" {
		target = "*"
	}
	if !s.manages(ctx, db, a, "connection", target) {
		return false
	}
	if a.Admin || id == "" {
		return true
	}
	rows, e := db.Query(ctx, `SELECT DISTINCT principal_id FROM destinations WHERE scope=$1 AND connection_id=$2`, a.Scope, id)
	if e != nil {
		return false
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return false
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return false
	}
	for _, pid := range ids {
		if !s.mayManageDestination(ctx, db, a, id, pid) {
			return false
		}
	}
	return true
}
