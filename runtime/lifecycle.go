package runtime

import (
	"context"
	"fmt"
	"github.com/beepgrid/beepgrid/internal/engine"
	"io"
	"time"
)

type ErasureStatus = engine.ErasureStatus

// These operator-only primitives must be protected separately from customer
// credentials by their caller. No customer HTTP route exposes them.
func (s *Service) ExportWorkspace(ctx context.Context, id string, output io.Writer) error {
	if !workspaceID.MatchString(id) {
		return fmt.Errorf("invalid workspace identity")
	}
	return s.store.ExportWorkspace(ctx, id+"/production", output)
}
func (s *Service) ReserveWorkspaceErasure(ctx context.Context, id, operation string, version int64, due time.Time) error {
	if !workspaceID.MatchString(id) {
		return fmt.Errorf("invalid workspace identity")
	}
	return s.store.ReserveWorkspaceErasure(ctx, id+"/production", operation, version, due)
}
func (s *Service) EraseWorkspaceStep(ctx context.Context, id, operation string) (bool, error) {
	if !workspaceID.MatchString(id) {
		return false, fmt.Errorf("invalid workspace identity")
	}
	return s.store.EraseWorkspaceStep(ctx, id+"/production", operation)
}
func (s *Service) WorkspaceErasure(ctx context.Context, id string) (ErasureStatus, error) {
	if !workspaceID.MatchString(id) {
		return ErasureStatus{}, fmt.Errorf("invalid workspace identity")
	}
	return s.store.WorkspaceErasure(ctx, id+"/production")
}
