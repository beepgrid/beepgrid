package engine

import (
	"context"
	"github.com/jackc/pgx/v5"
	"sort"
)

// Blocking is an explicit declaration that the requesting agent cannot continue
// until this single addressee answers. Ordinary open requests are not dependencies.
type BlockingDependency struct {
	RequestID      string    `json:"request_id"`
	ConversationID string    `json:"conversation_id"`
	From           Principal `json:"from"`
	To             Principal `json:"to"`
}
type AgentDeadlock struct {
	Dependencies []BlockingDependency `json:"dependencies"`
}

func (s *Store) SetRequestBlocking(ctx context.Context, a Actor, id string, blocking bool) (Request, error) {
	var out Request
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		var err error
		out, err = s.request(ctx, tx, a, id)
		if err != nil {
			return err
		}
		if a.ID != out.RequesterID || a.Kind != "agent" {
			return Denied()
		}
		if out.State != "open" {
			return Conflict("Only an open request can block work.")
		}
		if blocking {
			var valid bool
			err = tx.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(p.kind='agent') FROM request_audience ra JOIN principals p ON p.scope=ra.scope AND p.id=ra.principal_id WHERE ra.scope=$1 AND ra.request_id=$2`, a.Scope, id).Scan(&valid)
			if err != nil {
				return err
			}
			if !valid {
				return Bad("Blocking dependencies require one individual agent recipient.")
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE requests SET blocking=$3 WHERE scope=$1 AND id=$2`, a.Scope, id, blocking); err != nil {
			return err
		}
		out.Blocking = blocking
		return s.requestEvent(ctx, tx, a, out, "request.dependency_changed")
	})
	return out, err
}
func (s *Store) deadlocks(ctx context.Context, db DB, a Actor, allowed map[string]bool) ([]AgentDeadlock, error) {
	rows, err := db.Query(ctx, `SELECT r.id,ra.conversation_id,r.requester_id,ra.principal_id FROM requests r JOIN request_audience ra ON ra.scope=r.scope AND ra.request_id=r.id JOIN principals p ON p.scope=r.scope AND p.id=r.requester_id JOIN principals target ON target.scope=ra.scope AND target.id=ra.principal_id WHERE r.scope=$1 AND r.blocking AND r.state='open' AND r.expires_at>now() AND NOT p.disabled AND NOT target.disabled ORDER BY r.id LIMIT 1001`, a.Scope)
	if err != nil {
		return nil, err
	}
	type ref struct{ id, conv, from, to string }
	refs := []ref{}
	for rows.Next() {
		var e ref
		if err = rows.Scan(&e.id, &e.conv, &e.from, &e.to); err != nil {
			rows.Close()
			return nil, err
		}
		refs = append(refs, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Avoid claiming a complete graph if the bounded graph read was truncated.
	if len(refs) > 1000 {
		return []AgentDeadlock{}, nil
	}
	ids, convs := []string{}, []string{}
	for _, e := range refs {
		ids = append(ids, e.from, e.to)
		convs = append(convs, e.conv)
	}
	people, err := principalsByIDs(ctx, db, a.Scope, ids)
	if err != nil {
		return nil, err
	}
	visible, err := s.readableEventConversations(ctx, a, convs)
	if err != nil {
		return nil, err
	}
	edges := []BlockingDependency{}
	graph := map[string][]string{}
	for _, e := range refs {
		if !allowed[e.from] || !allowed[e.to] || !visible[e.conv] || (!a.Admin && a.ID != e.from && a.ID != e.to) {
			continue
		}
		from, ok := people[e.from]
		if !ok {
			continue
		}
		to, ok := people[e.to]
		if !ok {
			continue
		}
		edges = append(edges, BlockingDependency{e.id, e.conv, from, to})
		graph[e.from] = append(graph[e.from], e.to)
	}
	// Tarjan finds strongly connected blocking components in O(V+E). Visibility
	// is applied before traversal, so hidden dependencies cannot reveal a cycle.
	next := 0
	index := map[string]int{}
	low := map[string]int{}
	on := map[string]bool{}
	stack := []string{}
	out := []AgentDeadlock{}
	component := map[string]int{}
	componentSizes := []int{}
	var visit func(string)
	visit = func(v string) {
		next++
		index[v] = next
		low[v] = next
		stack = append(stack, v)
		on[v] = true
		for _, w := range graph[v] {
			if index[w] == 0 {
				visit(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if on[w] && index[w] < low[v] {
				low[v] = index[w]
			}
		}
		if low[v] != index[v] {
			return
		}
		members := map[string]bool{}
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			on[w] = false
			members[w] = true
			if w == v {
				break
			}
		}
		n := len(componentSizes)
		componentSizes = append(componentSizes, len(members))
		for id := range members {
			component[id] = n
		}

	}
	keys := []string{}
	for id := range graph {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		if index[id] == 0 {
			visit(id)
		}
	}
	grouped := make([]AgentDeadlock, len(componentSizes))
	for _, e := range edges {
		n := component[e.From.ID]
		if n == component[e.To.ID] && (componentSizes[n] > 1 || e.From.ID == e.To.ID) {
			grouped[n].Dependencies = append(grouped[n].Dependencies, e)
		}
	}
	for _, c := range grouped {
		if len(c.Dependencies) > 0 {
			out = append(out, c)
		}
	}
	return out, nil
}
