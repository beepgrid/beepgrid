package engine

import (
	"context"
	"encoding/json"
)

func (s *Store) RefreshActor(ctx context.Context, a Actor) (Actor, error) {
	var out Actor
	var meta []byte
	e := s.Pool.QueryRow(ctx, `SELECT p.id,p.kind,p.external_id,p.name,p.metadata,p.created_at,c.scope,c.id,c.kind,c.prefix,COALESCE(i.id,'') FROM credentials c JOIN principals p ON p.id=c.principal_id AND p.scope=c.scope LEFT JOIN instances i ON i.credential_id=c.id WHERE c.scope=$1 AND c.id=$2 AND NOT c.revoked AND NOT p.disabled AND (c.expires_at IS NULL OR c.expires_at>now())`, a.Scope, a.CredentialID).Scan(&out.ID, &out.Kind, &out.ExternalID, &out.Name, &meta, &out.CreatedAt, &out.Scope, &out.CredentialID, &out.CredentialKind, &out.Prefix, &out.InstanceID)
	if e != nil {
		return out, Denied()
	}
	_ = json.Unmarshal(meta, &out.Metadata)
	out.Admin = out.CredentialKind == "admin"
	if out.CredentialKind == "browser" {
		_ = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, out.Scope, out.ID).Scan(&out.Admin)
	}
	return out, nil
}
func (s *Store) canRead(ctx context.Context, db DB, a Actor, id string) bool {
	if !canReadConversation(ctx, db, a, id) {
		return false
	}
	if a.Admin {
		return true
	}
	rows, e := db.Query(ctx, `SELECT p.id,p.kind FROM participants cp JOIN principals p ON p.id=cp.principal_id AND p.scope=cp.scope WHERE cp.scope=$1 AND cp.conversation_id=$2 AND p.id<>$3`, a.Scope, id, a.ID)
	if e != nil {
		return false
	}
	type ref struct{ id, kind string }
	targets := []ref{}
	for rows.Next() {
		var p ref
		if rows.Scan(&p.id, &p.kind) != nil {
			rows.Close()
			return false
		}
		targets = append(targets, p)
	}
	rows.Close()
	for _, p := range targets {
		if !s.allowed(ctx, db, a, "view", Target{p.kind, p.id}, "", "") {
			return false
		}
	}
	return len(targets) > 0
}
