package engine

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type AssistanceSettings struct {
	AutomaticAnswers   bool  `json:"automatic_answers"`
	Overseer           bool  `json:"overseer"`
	Version            int64 `json:"version"`
	MatcherConfigured  bool  `json:"matcher_configured"`
	OverseerConfigured bool  `json:"overseer_configured"`
}
type AssistanceUpdate struct {
	AutomaticAnswers *bool `json:"automatic_answers"`
	Overseer         *bool `json:"overseer"`
}

func (s *Store) assistanceSettings(ctx context.Context, db DB, scope string) (AssistanceSettings, error) {
	out := AssistanceSettings{MatcherConfigured: s.AutomaticMatcher != nil, OverseerConfigured: s.OverseerPlanner != nil}
	err := db.QueryRow(ctx, `SELECT automatic_answers,overseer_enabled,assistance_version FROM scopes WHERE id=$1`, scope).Scan(&out.AutomaticAnswers, &out.Overseer, &out.Version)
	return out, normalize(err)
}
func (s *Store) AssistanceSettings(ctx context.Context, a Actor) (AssistanceSettings, error) {
	return s.assistanceSettings(ctx, s.Pool, a.Scope)
}
func (s *Store) UpdateAssistance(ctx context.Context, a Actor, in AssistanceUpdate) (AssistanceSettings, error) {
	var out AssistanceSettings
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !a.Admin {
			return Denied()
		}
		var err error
		out, err = s.assistanceSettings(ctx, tx, a.Scope)
		if err != nil {
			return err
		}
		previous := out
		if in.AutomaticAnswers != nil {
			out.AutomaticAnswers = *in.AutomaticAnswers
		}
		if in.Overseer != nil {
			out.Overseer = *in.Overseer
		}
		if in.AutomaticAnswers != nil && *in.AutomaticAnswers && s.AutomaticMatcher == nil {
			return Conflict("Configure a semantic matching provider before enabling automatic answers.")
		}
		if in.Overseer != nil && *in.Overseer && s.OverseerPlanner == nil {
			return Conflict("Configure an overseer provider before enabling the overseer.")
		}
		if previous.AutomaticAnswers == out.AutomaticAnswers && previous.Overseer == out.Overseer {
			return nil
		}
		if err = tx.QueryRow(ctx, `UPDATE scopes SET automatic_answers=$2,overseer_enabled=$3,assistance_version=assistance_version+1 WHERE id=$1 RETURNING assistance_version`, a.Scope, out.AutomaticAnswers, out.Overseer).Scan(&out.Version); err != nil {
			return err
		}
		// Re-enabling never revives stale model decisions or unconfirmed command plans.
		if _, err = tx.Exec(ctx, `UPDATE answer_match_jobs SET state='cancelled' WHERE scope=$1 AND state IN ('queued','leased')`, a.Scope); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE deliveries SET next_at=now() WHERE scope=$1 AND state='queued' AND operation_id IN (SELECT request_id FROM answer_match_jobs WHERE scope=$1 AND state='cancelled')`, a.Scope); err != nil {
			return err
		}
		return event(ctx, tx, a, "assistance.settings_changed", "", "", map[string]any{"automatic_answers": out.AutomaticAnswers, "overseer": out.Overseer, "version": out.Version})
	})
	return out, err
}
