package runtime

import (
	"context"
	"fmt"
	"github.com/beepgrid/beepgrid/internal/engine"
)

type WorkspaceBudget = engine.WorkspaceBudget
type UsageUnit = engine.UsageUnit

// Operator-only composition seams; these are not public HTTP endpoints.
func (s *Service) ApplyWorkspaceBudget(ctx context.Context, id string, b WorkspaceBudget) error {
	if !workspaceID.MatchString(id) {
		return fmt.Errorf("invalid workspace identity")
	}
	return s.store.ApplyWorkspaceBudget(ctx, id+"/production", b)
}
func (s *Service) WorkspaceUsage(ctx context.Context, id string) ([]UsageUnit, error) {
	if !workspaceID.MatchString(id) {
		return nil, fmt.Errorf("invalid workspace identity")
	}
	return s.store.WorkspaceUsage(ctx, id+"/production")
}
