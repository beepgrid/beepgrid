package engine

import (
	"context"
	"encoding/json"
	"sync"
)

// A snapshot lives for one bounded operation only, never across HTTP requests,
// long-poll iterations or SSE refreshes. Writes take the workspace lock before using it.
type policySnapshotKey struct{}
type policySnapshot struct {
	mu          sync.Mutex
	rules       map[string][]Rule
	memberships map[string]map[string]bool
	assigned    map[string]map[string]bool
	channels    map[string]map[string]ProviderChannel
}

func withPolicySnapshot(ctx context.Context) context.Context {
	if ctx.Value(policySnapshotKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, policySnapshotKey{}, &policySnapshot{rules: map[string][]Rule{}, memberships: map[string]map[string]bool{}, assigned: map[string]map[string]bool{}, channels: map[string]map[string]ProviderChannel{}})
}

func assignedRoles(ctx context.Context, db DB, a Actor) (map[string]bool, error) {
	snapshot, _ := ctx.Value(policySnapshotKey{}).(*policySnapshot)
	key := a.Scope + "\x00" + a.ID
	if snapshot != nil {
		snapshot.mu.Lock()
		defer snapshot.mu.Unlock()
		if roles, ok := snapshot.assigned[key]; ok {
			return roles, nil
		}
	}
	rows, err := db.Query(ctx, `SELECT role_id FROM role_bindings WHERE scope=$1 AND subject_kind='principal' AND subject_id=$2
 UNION ALL SELECT b.role_id FROM memberships m JOIN role_bindings b ON b.scope=m.scope AND b.subject_kind='group' AND b.subject_id=m.group_id WHERE m.scope=$1 AND m.principal_id=$2`, a.Scope, a.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if snapshot != nil {
		snapshot.assigned[key] = out
	}
	return out, nil
}
func roleRules(ctx context.Context, db DB, a Actor) ([]Rule, error) {
	snapshot, _ := ctx.Value(policySnapshotKey{}).(*policySnapshot)
	key := a.Scope + "\x00" + a.ID
	if snapshot != nil {
		snapshot.mu.Lock()
		defer snapshot.mu.Unlock()
		if rules, ok := snapshot.rules[key]; ok {
			return rules, nil
		}
	}
	rows, e := db.Query(ctx, roleRulesSQL, a.Scope, a.ID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	all := []Rule{}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var rules []Rule
		if e = json.Unmarshal(raw, &rules); e != nil {
			return nil, e
		}
		all = append(all, rules...)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if snapshot != nil {
		snapshot.rules[key] = all
	}
	return all, nil
}
func targetGroupMember(ctx context.Context, db DB, scope, group, person string) bool {
	snapshot, _ := ctx.Value(policySnapshotKey{}).(*policySnapshot)
	if snapshot == nil {
		var member bool
		_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE scope=$1 AND principal_id=$2 AND (group_id=$3 OR $3='*'))`, scope, person, group).Scan(&member)
		return member
	}
	snapshot.mu.Lock()
	defer snapshot.mu.Unlock()
	key := scope + "\x00" + group
	members, ok := snapshot.memberships[key]
	if !ok {
		rows, e := db.Query(ctx, `SELECT principal_id FROM memberships WHERE scope=$1 AND (group_id=$2 OR $2='*')`, scope, group)
		if e != nil {
			return false
		}
		defer rows.Close()
		members = map[string]bool{}
		for rows.Next() {
			var id string
			if rows.Scan(&id) != nil {
				return false
			}
			members[id] = true
		}
		if rows.Err() != nil {
			return false
		}
		snapshot.memberships[key] = members
	}
	return members[person]
}

const roleRulesSQL = `SELECT r.rules FROM (
 SELECT role_id FROM role_bindings WHERE scope=$1 AND subject_kind='principal' AND subject_id=$2
 UNION ALL
 SELECT b.role_id FROM memberships m JOIN role_bindings b ON b.scope=m.scope AND b.subject_kind='group' AND b.subject_id=m.group_id
 WHERE m.scope=$1 AND m.principal_id=$2
 ) effective JOIN roles r ON r.scope=$1 AND r.id=effective.role_id`
