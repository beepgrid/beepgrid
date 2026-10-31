package runtime

import (
	"context"
	"fmt"
	"github.com/beepgrid/beepgrid/internal/engine"
	"time"
)

type RestoredWorkspace = engine.RestoredWorkspace

// Offline operator-only methods. The caller must keep the restored database
// isolated from application traffic until review and explicit resumption.
func (s *Service) HoldRestoredCell(ctx context.Context) ([]RestoredWorkspace, error) {
	return s.store.HoldRestoredCell(ctx)
}
func (s *Service) RecoverWorkspace(ctx context.Context, id, token string) error {
	if !workspaceID.MatchString(id) {
		return fmt.Errorf("invalid workspace identity")
	}
	return s.store.ReconcileRestored(ctx, id+"/production", token)
}

func (s *Service) ApplyRestoredWorkspaceAccess(ctx context.Context, id string, version int64, suspended bool, until time.Time) error {
	if !workspaceID.MatchString(id) || version < 1 {
		return fmt.Errorf("invalid restored workspace access")
	}
	return s.store.ApplyRestoredWorkspaceAccess(ctx, id+"/production", version, suspended, until)
}
