package engine

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type AssistanceSettings struct {
	Overseer           bool  `json:"overseer"`
	Version            int64 `json:"version"`
	OverseerConfigured bool  `json:"overseer_configured"`
}
type AssistanceUpdate struct {
	Version  int64 `json:"version,omitempty"`
	Overseer *bool `json:"overseer"`
}

func (s *Store) assistanceSettings(ctx context.Context, db DB, scope string) (AssistanceSettings, error) {
	out := AssistanceSettings{OverseerConfigured: s.OverseerPlanner != nil}
	err := db.QueryRow(ctx, `SELECT overseer_enabled,assistance_version FROM scopes WHERE id=$1`, scope).Scan(&out.Overseer, &out.Version)
	return out, normalize(err)
}
func (s *Store) AssistanceSettings(ctx context.Context, a Actor) (AssistanceSettings, error) {
	return s.assistanceSettings(ctx, s.Pool, a.Scope)
}
func (s *Store) UpdateAssistance(ctx context.Context, a Actor, in AssistanceUpdate) (AssistanceSettings, error) {
	var out AssistanceSettings
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if a.Kind != "person" || !s.manages(ctx, tx, a, "assistance", "workspace") {
			return Denied()
		}
		var err error
		out, err = s.assistanceSettings(ctx, tx, a.Scope)
		if err != nil {
			return err
		}
		if in.Version < 0 {
			return Bad("Configuration version must not be negative.")
		}
		if in.Version > 0 && in.Version != out.Version {
			return Conflict("Assistance settings changed. Reload before saving.")
		}
		previous := out
		if in.Overseer != nil {
			out.Overseer = *in.Overseer
		}
		if in.Overseer != nil && *in.Overseer && s.OverseerPlanner == nil {
			return Conflict("Configure a Coordinator provider before enabling Coordinator.")
		}
		if previous.Overseer == out.Overseer {
			return nil
		}
		if err = tx.QueryRow(ctx, `UPDATE scopes SET overseer_enabled=$2,assistance_version=assistance_version+1 WHERE id=$1 RETURNING assistance_version`, a.Scope, out.Overseer).Scan(&out.Version); err != nil {
			return err
		}
		// Settings revisions invalidate unconfirmed Coordinator plans.
		return event(ctx, tx, a, "assistance.settings_changed", "", "", map[string]any{"overseer": out.Overseer, "version": out.Version})
	})
	return out, err
}
