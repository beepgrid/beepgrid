package engine

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Positive versions opt into compare-and-swap. Zero retains legacy create/save
// compatibility. Reads always include a positive version; new clients preserve it.
func checkRevision(ctx context.Context, db DB, scope, table, id string, version int64) error {
	if version < 0 {
		return Bad("Configuration version must not be negative.")
	}
	if id == "" {
		if version != 0 {
			return Bad("New configuration must not supply an existing version.")
		}
		return nil
	}
	switch table {
	case "groups", "roles", "connections", "destinations":
	default:
		return Bad("Invalid configuration resource.")
	}
	var current int64
	e := db.QueryRow(ctx, `SELECT version FROM `+table+` WHERE scope=$1 AND id=$2`, scope, id).Scan(&current)
	if e == pgx.ErrNoRows {
		if version > 0 {
			return Missing()
		}
		return nil
	}
	if e != nil {
		return e
	}
	if version > 0 && version != current {
		return Conflict("Configuration changed. Reload it before saving.")
	}
	return nil
}
