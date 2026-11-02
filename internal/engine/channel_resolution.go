package engine

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// DeliveryRouting is private durable scheduling state, never Agent instructions.
type DeliveryRouting struct {
	Managed            bool   `json:"managed"`
	DestinationID      string `json:"destination_id,omitempty"`
	DestinationVersion int64  `json:"destination_version,omitempty"`
	Version            int64  `json:"version"`
	Position           int    `json:"position"`
	Copy               bool   `json:"copy"`
	FollowupSeconds    int    `json:"followup_seconds"`
}
type providerRoute struct {
	ID           string          `json:"delivery_id"`
	ConnectionID string          `json:"connection_id"`
	Address      string          `json:"address"`
	State        string          `json:"state"`
	Routing      DeliveryRouting `json:"routing"`
}
type channelCandidate struct {
	Destination
	Enabled bool
	Kind    string
	Inbound bool
}

func chooseChannelPolicy(p DeliveryPreferences, sender string, roles map[string]bool) *ChannelPolicy {
	for _, o := range p.Overrides {
		if o.Source.Kind == "agent" && o.Source.ID == sender {
			return &o.Policy
		}
	}
	for _, o := range p.Overrides {
		if o.Source.Kind == "group" && roles[o.Source.ID] {
			return &o.Policy
		}
	}
	return p.Default
}

// Resolve once for the batch, before persisting. Policy, enabled connections and
// current channel permissions are intersected; preferences never grant access.
func (s *Store) resolveProviderRoutes(ctx context.Context, db DB, a Actor, rs []recipient, conv, action, name string, descriptor *Action) (map[string][]providerRoute, error) {
	out := map[string][]providerRoute{}
	ids := make([]string, 0, len(rs))
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	rows, err := db.Query(ctx, `SELECT person_id,version,default_policy,overrides FROM delivery_preferences WHERE scope=$1 AND person_id=ANY($2::text[])`, a.Scope, ids)
	if err != nil {
		return nil, err
	}
	prefs := map[string]DeliveryPreferences{}
	for rows.Next() {
		var p DeliveryPreferences
		var def, ov []byte
		if err = rows.Scan(&p.PersonID, &p.Version, &def, &ov); err != nil {
			rows.Close()
			return nil, err
		}
		if len(def) > 0 {
			if err = json.Unmarshal(def, &p.Default); err != nil {
				rows.Close()
				return nil, err
			}
		}
		if err = json.Unmarshal(ov, &p.Overrides); err != nil {
			rows.Close()
			return nil, err
		}
		prefs[p.PersonID] = p
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	roles := map[string]bool{}
	if a.Kind == "agent" && len(prefs) > 0 {
		rows, err = db.Query(ctx, `SELECT g.id FROM memberships m JOIN groups g ON g.scope=m.scope AND g.id=m.group_id WHERE m.scope=$1 AND m.principal_id=$2 AND g.purpose='agent_role'`, a.Scope, a.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			roles[id] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	rows, err = db.Query(ctx, `SELECT d.principal_id,d.id,d.connection_id,d.address,d.mode,d.priority,d.version,c.enabled,c.kind FROM destinations d JOIN connections c ON c.scope=d.scope AND c.id=d.connection_id WHERE d.scope=$1 AND d.principal_id=ANY($2::text[]) ORDER BY d.priority,d.id`, a.Scope, ids)
	if err != nil {
		return nil, err
	}
	candidates := map[string][]channelCandidate{}
	for rows.Next() {
		var id string
		var d channelCandidate
		if err = rows.Scan(&id, &d.ID, &d.ConnectionID, &d.Address, &d.Mode, &d.Priority, &d.Version, &d.Enabled, &d.Kind); err != nil {
			rows.Close()
			return nil, err
		}
		candidates[id] = append(candidates[id], d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Only an actual incoming message establishes reply affinity. Outbound receipts
	// record provider thread IDs, but must not multiply future notifications.
	inbound := map[string]channelCandidate{}
	if conv != "" {
		rows, err = db.Query(ctx, `SELECT DISTINCT ON(t.person_id) t.person_id,t.connection_id,t.channel_id,c.enabled,c.kind FROM provider_threads t JOIN connections c ON c.scope=t.scope AND c.id=t.connection_id WHERE t.scope=$1 AND t.conversation_id=$2 AND t.person_id=ANY($3::text[]) AND t.last_inbound_at IS NOT NULL AND c.enabled ORDER BY t.person_id,t.last_inbound_at DESC,t.connection_id,t.channel_id`, a.Scope, conv, ids)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var d channelCandidate
			if err = rows.Scan(&id, &d.ConnectionID, &d.Address, &d.Enabled, &d.Kind); err != nil {
				rows.Close()
				return nil, err
			}
			d.Inbound = true
			inbound[id] = d
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	for _, r := range rs {
		p := prefs[r.ID]
		policy := chooseChannelPolicy(p, a.ID, roles)
		source := candidates[r.ID]
		selected := []channelCandidate{}
		mode, delay := "legacy", 0
		if policy != nil {
			mode, delay = policy.Mode, policy.FollowupSeconds
			byID := map[string]channelCandidate{}
			for _, d := range source {
				byID[d.ID] = d
			}
			for _, id := range policy.Destinations {
				if d, ok := byID[id]; ok {
					d.Mode = "primary"
					selected = append(selected, d)
				}
			}
		} else {
			selected = append(selected, source...)
			sort.SliceStable(selected, func(i, j int) bool { return selected[i].Mode == "primary" && selected[j].Mode != "primary" })
		}
		// An explicit recipient policy bounds affinity to selected installations. A
		// workspace-only preference therefore never leaks to an old provider thread.
		if d, ok := inbound[r.ID]; ok {
			permitted := policy == nil
			if policy != nil {
				for _, x := range selected {
					if x.ConnectionID == d.ConnectionID {
						permitted = true
						d.ID = x.ID
						d.Version = x.Version
						break
					}
				}
			}
			if permitted {
				if policy != nil {
					kept := selected[:0]
					for _, x := range selected {
						if x.ID != d.ID {
							kept = append(kept, x)
						}
					}
					selected = kept
				}
				selected = append([]channelCandidate{d}, selected...)
			}
		}
		seen := map[string]bool{}
		position := 0
		for _, d := range selected {
			key := d.ConnectionID + "\x00" + d.Address
			if seen[key] || !d.Enabled || !s.allowed(ctx, db, a, action, r.PolicyTarget, d.ConnectionID, name, descriptor) {
				continue
			}
			if d.Kind == "telegram" && !s.telegramDestination(ctx, db, a.Scope, d.ConnectionID, r.ID, d.Address) {
				continue
			}
			if d.Kind == "discord" && !s.discordDestination(ctx, db, a.Scope, d.ConnectionID, r.ID, d.Address) {
				continue
			}
			seen[key] = true
			copy := mode == "all" || mode == "legacy" && d.Mode == "copy"
			route := providerRoute{ID: ID("del"), ConnectionID: d.ConnectionID, Address: d.Address, State: "queued", Routing: DeliveryRouting{Managed: true, DestinationID: d.ID, DestinationVersion: d.Version, Version: p.Version, Position: position, Copy: copy, FollowupSeconds: delay}}
			if !copy {
				if position > 0 {
					route.State = "standby"
				}
				position++
			}
			out[r.ID] = append(out[r.ID], route)
		}
	}
	return out, nil
}
func (s *Store) advanceProviderRoute(ctx context.Context, db DB, scope string, d Delivery, failed bool) error {
	if !d.Routing.Managed || d.Routing.Copy {
		return nil
	}
	wait := 0
	if !failed {
		wait = d.Routing.FollowupSeconds
		request, ok := d.Payload["request"].(map[string]any)
		if wait == 0 || !ok || (request["kind"] != "question" && request["kind"] != "approval") {
			_, err := db.Exec(ctx, `UPDATE deliveries SET state='cancelled',last_error='Preferred channel accepted notification' WHERE scope=$1 AND operation_id=$2 AND recipient_id=$3 AND state='standby'`, scope, d.OperationID, d.RecipientID)
			return err
		}
	}
	// The row is scheduled durably. Normal claims recheck unresolved request state,
	// current authorization, identity links, destination existence and policy edits.
	_, err := db.Exec(ctx, `UPDATE deliveries SET state='queued',next_at=now()+make_interval(secs=>$5::double precision) WHERE id=(SELECT id FROM deliveries WHERE scope=$1 AND operation_id=$2 AND recipient_id=$3 AND state='standby' AND routing->>'managed'='true' AND routing->>'copy'='false' AND (routing->>'position')::int>$4 ORDER BY (routing->>'position')::int,id LIMIT 1)`, scope, d.OperationID, d.RecipientID, d.Routing.Position, wait)
	return err
}
func insertProviderRoute(ctx context.Context, db DB, scope, op, recipient string, payload map[string]any, expiry time.Time, r providerRoute) error {
	_, err := db.Exec(ctx, `INSERT INTO deliveries(id,scope,operation_id,recipient_id,connection_id,address,payload,expires_at,state,routing) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, r.ID, scope, op, recipient, r.ConnectionID, r.Address, JSON(payload), expiry, r.State, JSON(r.Routing))
	return err
}
