package engine

import (
	"context"
	"encoding/json"
)

// Bulk evaluation has the same membership and all-other-participants View
// requirement as canRead, without a database round trip per event conversation.
func (s *Store) readableEventConversations(ctx context.Context, a Actor, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 || a.CredentialKind == "enrollment" {
		return out, nil
	}
	rows, e := s.Pool.Query(ctx, `SELECT c.id,EXISTS(SELECT 1 FROM participants own WHERE own.scope=c.scope AND own.conversation_id=c.id AND own.principal_id=$3),COALESCE(jsonb_agg(jsonb_build_object('kind',p.kind,'id',p.id)) FILTER(WHERE p.id<>$3),'[]') FROM conversations c LEFT JOIN participants cp ON cp.scope=c.scope AND cp.conversation_id=c.id LEFT JOIN principals p ON p.scope=cp.scope AND p.id=cp.principal_id WHERE c.scope=$1 AND c.id=ANY($2::text[]) GROUP BY c.id`, a.Scope, ids, a.ID)
	if e != nil {
		return nil, e
	}
	type candidate struct {
		id     string
		member bool
		others []Target
	}
	all := []candidate{}
	for rows.Next() {
		var c candidate
		var raw []byte
		if e = rows.Scan(&c.id, &c.member, &raw); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(raw, &c.others); e != nil {
			rows.Close()
			return nil, e
		}
		all = append(all, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for _, c := range all {
		if a.Admin {
			out[c.id] = true
			continue
		}
		valid := c.member && len(c.others) > 0
		for _, p := range c.others {
			if !s.allowed(ctx, s.Pool, a, "view", p, "", "") {
				valid = false
				break
			}
		}
		out[c.id] = valid
	}
	return out, nil
}
