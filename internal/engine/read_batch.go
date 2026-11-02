package engine

import "context"

func distinctIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// Only requested, scoped identities are loaded. Callers still enforce their
// own visibility rules; a directory lookup is not permission to disclose it.
func principalsByIDs(ctx context.Context, db DB, scope string, ids []string) (map[string]Principal, error) {
	out := map[string]Principal{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `SELECT `+principalFields+` FROM principals WHERE scope=$1 AND id=ANY($2::text[])`, scope, distinctIDs(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPrincipal(rows)
		if err != nil {
			return nil, err
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}

func (s *Store) visibleGroupMembers(ctx context.Context, a Actor, groups []Group) ([]Group, error) {
	visible := []Group{}
	ids := []string{}
	for _, g := range groups {
		if a.Admin || s.allowed(ctx, s.Pool, a, "view", Target{"group", g.ID}, "", "") || s.manages(ctx, s.Pool, a, "group", g.ID) || contains(g.Members, a.ID) {
			visible = append(visible, g)
			ids = append(ids, g.Members...)
		}
	}
	people, err := principalsByIDs(ctx, s.Pool, a.Scope, ids)
	if err != nil {
		return nil, err
	}
	for i, g := range visible {
		members := []string{}
		for _, id := range g.Members {
			if p, ok := people[id]; ok && s.inspectIdentity(ctx, s.Pool, a, p) {
				members = append(members, id)
			}
		}
		g.Member = contains(g.Members, a.ID)
		g.MembersComplete = len(members) == len(g.Members)
		g.Members = members
		visible[i] = g
	}
	return visible, nil
}

// Listing assignments does not grant authority to create them. This read-only
// proof retains protected-admin and managed-subject checks from mayAssign.
func (s *Store) visibleBindings(ctx context.Context, a Actor, bindings []Binding) ([]Binding, error) {
	if a.Admin {
		return bindings, nil
	}
	ids := []string{}
	groups := []string{}
	for _, b := range bindings {
		if b.SubjectKind == "principal" {
			if b.SubjectID != a.ID {
				ids = append(ids, b.SubjectID)
			}
		} else if b.SubjectKind == "group" {
			groups = append(groups, b.SubjectID)
		}
	}
	people, err := principalsByIDs(ctx, s.Pool, a.Scope, ids)
	if err != nil {
		return nil, err
	}
	protected := map[string]bool{}
	checkProtected := []string{}
	for _, b := range bindings {
		if b.SubjectKind == "principal" && b.SubjectID != a.ID {
			if p, ok := people[b.SubjectID]; ok && s.manages(ctx, s.Pool, a, "role_binding", b.RoleID) && s.manages(ctx, s.Pool, a, p.Kind, p.ID) {
				checkProtected = append(checkProtected, p.ID)
			}
		}
	}
	if len(checkProtected) > 0 {
		rows, e := s.Pool.Query(ctx, `SELECT DISTINCT principal_id FROM credentials WHERE scope=$1 AND principal_id=ANY($2::text[]) AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now())`, a.Scope, distinctIDs(checkProtected))
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return nil, e
			}
			protected[id] = true
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	exists := map[string]bool{}
	member := map[string]bool{}
	if len(groups) > 0 {
		rows, e := s.Pool.Query(ctx, `SELECT g.id,EXISTS(SELECT 1 FROM memberships m WHERE m.scope=g.scope AND m.group_id=g.id AND m.principal_id=$3) FROM groups g WHERE g.scope=$1 AND g.id=ANY($2::text[])`, a.Scope, groups, a.ID)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var id string
			var yes bool
			if e = rows.Scan(&id, &yes); e != nil {
				rows.Close()
				return nil, e
			}
			exists[id] = true
			member[id] = yes
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	out := []Binding{}
	for _, b := range bindings {
		if b.SubjectKind == "principal" && b.SubjectID == a.ID {
			out = append(out, b)
			continue
		}
		target := Target{}
		if b.SubjectKind == "group" && exists[b.SubjectID] {
			target = Target{"group", b.SubjectID}
		} else if b.SubjectKind == "principal" {
			if p, ok := people[b.SubjectID]; ok {
				target = Target{p.Kind, p.ID}
			}
		}
		if target.Kind == "" {
			continue
		}
		if (s.manages(ctx, s.Pool, a, "role_binding", b.RoleID) && s.manages(ctx, s.Pool, a, target.Kind, target.ID) && (target.Kind == "group" || !protected[target.ID])) || (b.SubjectKind == "group" && member[b.SubjectID]) {
			out = append(out, b)
		}
	}
	return out, nil
}
