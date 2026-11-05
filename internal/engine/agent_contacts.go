package engine

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type AgentContactsInput struct {
	Targets []Target `json:"targets"`
}
type AgentContact struct {
	Target  Target         `json:"target"`
	Name    string         `json:"name"`
	Message bool           `json:"message"`
	Ask     bool           `json:"ask"`
	Routes  []PreviewRoute `json:"routes"`
}
type AgentContactsResult struct {
	Contacts   []AgentContact `json:"contacts"`
	ObservedAt time.Time      `json:"observed_at"`
}

// This is an observation of current contact policy, never an agent credential
// or permission to send as it. Both the viewer and subject must see a contact.
func (s *Store) AgentContacts(ctx context.Context, caller Actor, id string, in AgentContactsInput) (AgentContactsResult, error) {
	out := AgentContactsResult{Contacts: []AgentContact{}, ObservedAt: time.Now().UTC()}
	if len(in.Targets) > 100 {
		return out, Bad("Inspect up to 100 contacts at a time.")
	}
	err := s.tx(ctx, caller, func(db pgx.Tx) error {
		ctx = withPolicySnapshot(ctx)
		p, err := principal(ctx, db, caller.Scope, id)
		if err != nil || p.Kind != "agent" || p.Disabled || !s.inspectAccess(ctx, db, caller, p) {
			return Missing()
		}
		subject := Actor{Principal: p, Scope: caller.Scope, CredentialKind: "api"}
		ids := []string{}
		seen := map[string]bool{}
		for _, t := range in.Targets {
			if t.Kind != "person" && t.Kind != "agent" && t.Kind != "service" {
				return Bad("Choose individual people, agents or software services.")
			}
			if !seen[t.ID] {
				ids = append(ids, t.ID)
				seen[t.ID] = true
			}
		}
		people, err := principalsByIDs(ctx, db, caller.Scope, ids)
		if err != nil {
			return err
		}
		type route struct {
			connection, kind, mode, address string
			enabled                         bool
		}
		routes := map[string][]route{}
		rows, err := db.Query(ctx, `SELECT d.principal_id,c.id,c.kind,d.mode,c.enabled,d.address FROM destinations d JOIN connections c ON c.scope=d.scope AND c.id=d.connection_id WHERE d.scope=$1 AND d.principal_id=ANY($2::text[]) ORDER BY d.priority,d.id`, caller.Scope, ids)
		if err != nil {
			return err
		}
		for rows.Next() {
			var pid string
			var r route
			if err = rows.Scan(&pid, &r.connection, &r.kind, &r.mode, &r.enabled, &r.address); err != nil {
				rows.Close()
				return err
			}
			routes[pid] = append(routes[pid], r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		seen = map[string]bool{}
		for _, t := range in.Targets {
			peer, exists := people[t.ID]
			if !exists || seen[t.ID] || peer.Kind != t.Kind || peer.Disabled || !s.inspectIdentity(ctx, db, caller, peer) || !s.allowed(ctx, db, subject, "view", t, "", "") {
				continue
			}
			seen[t.ID] = true
			c := AgentContact{Target: t, Name: peer.Name, Message: s.allowed(ctx, db, subject, "message", t, "*", ""), Ask: s.allowed(ctx, db, subject, "ask", t, "*", ""), Routes: []PreviewRoute{}}
			if !c.Message && !c.Ask {
				continue
			}
			if s.allowed(ctx, db, subject, "message", t, "", "") || s.allowed(ctx, db, subject, "ask", t, "", "") {
				c.Routes = append(c.Routes, PreviewRoute{Kind: "native", Mode: "primary", Enabled: true, Permitted: true})
			}
			for _, r := range routes[peer.ID] {
				visible := caller.Admin || caller.ID == peer.ID || s.allowed(ctx, db, caller, "view", Target{"destination", peer.ID}, "", "") || s.mayManageDestination(ctx, db, caller, r.connection, peer.ID)
				permitted := s.allowed(ctx, db, subject, "message", t, r.connection, "") || s.allowed(ctx, db, subject, "ask", t, r.connection, "")
				if visible && permitted && s.providerChannelAllowed(ctx, db, subject, r.connection, r.address) {
					c.Routes = append(c.Routes, PreviewRoute{Kind: r.kind, Mode: r.mode, Enabled: r.enabled, Permitted: true})
				}
			}
			out.Contacts = append(out.Contacts, c)
		}
		return nil
	})
	return out, err
}
