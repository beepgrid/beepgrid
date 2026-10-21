package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestWorkspaceAdmissionCancellationAndReclamation(t *testing.T) {
	var g workspaceAdmission
	release, e := g.acquire(context.Background(), "blocked")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if acquired, e := g.acquire(ctx, "blocked"); e == nil {
		acquired.release()
		t.Fatal("blocked scope did not honor cancellation")
	}
	other, e := g.acquire(context.Background(), "other")
	if e != nil {
		t.Fatal(e)
	}
	other.release()
	release.release()
	var tasks sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		tasks.Add(1)
		go func(worker int) {
			defer tasks.Done()
			for i := 0; i < 500; i++ {
				done, e := g.acquire(context.Background(), fmt.Sprintf("scope-%d-%d", worker, i))
				if e != nil {
					t.Error(e)
					return
				}
				done.release()
			}
		}(worker)
	}
	tasks.Wait()
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.active) != 0 {
		t.Fatal("idle workspaces retained", len(g.active))
	}
}

func TestWorkspaceAdmissionBoundedPipeline(t *testing.T) {
	g := workspaceAdmission{capacity: 2}
	first, e := g.acquire(context.Background(), "one")
	if e != nil {
		t.Fatal(e)
	}
	second, e := g.acquire(context.Background(), "one")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if third, e := g.acquire(ctx, "one"); e == nil {
		third.release()
		t.Fatal("workspace exceeded its pipeline")
	}
	other, e := g.acquire(context.Background(), "two")
	if e != nil {
		t.Fatal(e)
	}
	other.release()
	second.release()
	first.release()
	if len(g.active) != 0 {
		t.Fatal("pipeline leaked workspace state")
	}
}
