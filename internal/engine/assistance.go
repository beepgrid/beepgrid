package engine

import (
	"context"
	"fmt"
	"time"
)


type FleetStatus struct {
	Agent     Principal  `json:"agent"`
	Instances []Instance `json:"instances"`
	Fresh     bool       `json:"fresh"`
}

func (s *Store) FleetStatus(ctx context.Context, a Actor, target *Target) (map[string]any, error) {
	return s.fleetStatus(ctx, s.Pool, a, target)
}
func (s *Store) fleetStatus(ctx context.Context, db DB, a Actor, target *Target) (map[string]any, error) {
	ctx = withPolicySnapshot(ctx)
	agents, e := s.listPrincipals(ctx, db, a, "agent")
	if e != nil {
		return nil, e
	}
	allowed := map[string]bool{}
	if target != nil {
		if target.Kind != "group" && target.Kind != "agent" {
			return nil, Bad("Status targets must be an agent or group.")
		}
		rs, e := s.resolve(ctx, db, a, *target, "view", "")
		if e != nil {
			return nil, e
		}
		for _, r := range rs {
			if r.Kind == "agent" {
				allowed[r.ID] = true
			}
		}
	} else {
		for _, p := range agents {
			if p.ID == a.ID || s.allowed(ctx, db, a, "view", Target{"agent", p.ID}, "", "") {
				allowed[p.ID] = true
			}
		}
	}
	if len(allowed) > 100 {
		return nil, Bad("Select at most 100 agents for one status query.")
	}
	ids := []string{}
	for _, p := range agents {
		if allowed[p.ID] {
			ids = append(ids, p.ID)
		}
	}
	byAgent, e := s.instancesForAgentsDB(ctx, db, a.Scope, ids)
	if e != nil {
		return nil, e
	}
	out := []FleetStatus{}
	fresh := 0
	for _, p := range agents {
		if !allowed[p.ID] {
			continue
		}
		instances := byAgent[p.ID]
		isFresh := false
		for _, i := range instances {
			if i.Presence == "connected" {
				isFresh = true
			}
		}
		if isFresh {
			fresh++
		}
		out = append(out, FleetStatus{p, instances, isFresh})
	}
	return map[string]any{"agents": out, "total": len(out), "fresh": fresh, "missing_or_stale": len(out) - fresh, "as_of": time.Now(), "summary": fmt.Sprintf("%d of %d permitted agents have a recently connected instance. Progress is worker-reported; overlapping counts are not added.", fresh, len(out)), "mode": "read_only", "commands_supported": false}, nil
}
