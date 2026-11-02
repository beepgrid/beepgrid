package engine

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Appearance is presentation-only. It cannot change membership or authority.
type AppearanceInput struct {
	Target  Target `json:"target"`
	Icon    string `json:"icon"`
	Version int64  `json:"version"`
}

func (s *Store) SaveAppearance(ctx context.Context, a Actor, in AppearanceInput) (AppearanceInput, error) {
	prefix := "mark-"
	if in.Target.Kind == "group" {
		prefix = "role-"
	} else if in.Target.Kind != "agent" {
		return in, Bad("Choose an Agent or Agent role.")
	}
	if in.Icon != "" {
		index, err := strconv.Atoi(strings.TrimPrefix(in.Icon, prefix))
		if err != nil || index < 0 || index >= 12 || in.Icon != prefix+strconv.Itoa(index) {
			return in, Bad("Choose an icon from the provided pack.")
		}
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if in.Target.Kind == "group" {
			if !s.manages(ctx, tx, a, "group", in.Target.ID) {
				return Denied()
			}
			if err := checkRevision(ctx, tx, a.Scope, "groups", in.Target.ID, in.Version); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `UPDATE groups SET icon=$3,version=version+1 WHERE scope=$1 AND id=$2 RETURNING version`, a.Scope, in.Target.ID, in.Icon).Scan(&in.Version); err != nil {
				return normalize(err)
			}
			return event(ctx, tx, a, "group.saved", "", in.Target.ID, map[string]any{"id": in.Target.ID, "appearance_only": true})
		}
		p, err := principal(ctx, tx, a.Scope, in.Target.ID)
		if err != nil || p.Kind != "agent" {
			return Missing()
		}
		if !s.managesIdentity(ctx, tx, a, p) {
			return Denied()
		}
		if _, err := tx.Exec(ctx, `UPDATE principals SET metadata=metadata||$3::jsonb WHERE scope=$1 AND id=$2`, a.Scope, p.ID, JSON(map[string]string{"appearance_icon": in.Icon})); err != nil {
			return err
		}
		return event(ctx, tx, a, "agent.updated", "", p.ID, map[string]string{"id": p.ID, "appearance_icon": in.Icon})
	})
	return in, err
}
