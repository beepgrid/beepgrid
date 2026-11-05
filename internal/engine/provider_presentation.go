package engine

import (
	"context"
	"strings"
	"unicode"
)

func deliveryIdentity(a Actor) map[string]any {
	data := map[string]any{"sender": map[string]string{"id": a.ID, "kind": a.Kind, "name": a.Name}}
	if a.MessageSource != nil {
		data["source"] = a.MessageSource
	}
	return data
}

// Attribution is resolved by the server from the authenticated sender. Models
// do not need to add a signature or know which provider will deliver a message.
func (s *Store) providerAttribution(ctx context.Context, connection string, payload map[string]any) (string, error) {
	id := ""
	if m, ok := payload["message"].(map[string]any); ok {
		id, _ = m["sender_id"].(string)
	}
	if q, ok := payload["request"].(map[string]any); ok {
		id, _ = q["requester_id"].(string)
	}
	if id == "" {
		return "", nil
	}
	var provider, kind, name string
	if err := s.Pool.QueryRow(ctx, `SELECT c.kind,p.kind,p.name FROM principals p JOIN connections c ON c.scope=p.scope WHERE c.id=$1 AND p.id=$2`, connection, id).Scan(&provider, &kind, &name); err != nil {
		return "", err
	}
	if kind != "agent" {
		return "", nil
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, name)
	if provider == "slack" {
		name = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(name)
	}
	return "Agent: " + name + "\n\n", nil
}
