package engine

import (
	"context"
	"sync"
)

type workspaceWaiter struct {
	token chan struct{}
	users int
}
type workspaceAdmission struct {
	mu       sync.Mutex
	active   map[string]*workspaceWaiter
	capacity int
}

var workspaceWaiters sync.Pool

type workspaceLease struct {
	g     *workspaceAdmission
	entry *workspaceWaiter
	scope string
}

func (g *workspaceAdmission) drop(scope string, entry *workspaceWaiter) {
	g.mu.Lock()
	entry.users--
	if entry.users == 0 {
		delete(g.active, scope)
		workspaceWaiters.Put(entry)
	}
	g.mu.Unlock()
}
func (l workspaceLease) release() { <-l.entry.token; l.g.drop(l.scope, l.entry) }

// PostgreSQL remains the cross-replica authority. This local gate only avoids
// using several database connections to wait on one workspace's existing row
// lock, leaving the shared pool available to unrelated workspaces.
func (g *workspaceAdmission) acquire(ctx context.Context, scope string) (workspaceLease, error) {
	return g.acquireCapacity(ctx, scope, max(1, g.capacity))
}

func (g *workspaceAdmission) acquireCapacity(ctx context.Context, scope string, capacity int) (workspaceLease, error) {
	if e := ctx.Err(); e != nil {
		return workspaceLease{}, e
	}
	g.mu.Lock()
	if g.active == nil {
		g.active = make(map[string]*workspaceWaiter)
	}
	entry := g.active[scope]
	if entry == nil {
		if reusable := workspaceWaiters.Get(); reusable != nil {
			entry = reusable.(*workspaceWaiter)
		} else {
			entry = &workspaceWaiter{token: make(chan struct{}, 1)}
		}
		if cap(entry.token) != capacity {
			entry.token = make(chan struct{}, capacity)
		}
		g.active[scope] = entry
	}
	entry.users++
	g.mu.Unlock()
	select {
	case entry.token <- struct{}{}:
		lease := workspaceLease{g, entry, scope}
		if e := ctx.Err(); e != nil {
			lease.release()
			return workspaceLease{}, e
		}
		return lease, nil
	case <-ctx.Done():
		g.drop(scope, entry)
		return workspaceLease{}, ctx.Err()
	}
}
