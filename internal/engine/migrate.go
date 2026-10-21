package engine

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"regexp"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrations embed.FS

const SchemaVersion = 13

func pool(ctx context.Context, url, name string) (*pgxpool.Pool, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(name) {
		return nil, Bad("Invalid database schema")
	}
	cfg, e := pgxpool.ParseConfig(url)
	if e != nil {
		return nil, e
	}
	cfg.MaxConns = 12
	cfg.ConnConfig.RuntimeParams["search_path"] = name + ",public"
	return pgxpool.NewWithConfig(ctx, cfg)
}

// Migrate is run by a deployment credential with DDL permission. Runtime serving does not require DDL.
func Migrate(ctx context.Context, url, name string) error {
	p, e := pool(ctx, url, name)
	if e != nil {
		return e
	}
	defer p.Close()
	tx, e := p.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, name); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+name); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version integer PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now()); ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum text NOT NULL DEFAULT ''`); e != nil {
		return e
	}
	var highest int
	if e = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&highest); e != nil {
		return e
	}
	if highest > SchemaVersion {
		return fmt.Errorf("database version %d is newer than this binary", highest)
	}
	for version := 1; version <= SchemaVersion; version++ {
		raw, e := migrations.ReadFile(fmt.Sprintf("migrations/%03d.sql", version))
		if e != nil {
			return e
		}
		sum := sha256.Sum256(raw)
		checksum := hex.EncodeToString(sum[:])
		var old string
		e = tx.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=$1`, version).Scan(&old)
		if e == nil {
			if old != "" && old != checksum {
				return fmt.Errorf("migration %d checksum changed", version)
			}
			if old == "" {
				if _, e = tx.Exec(ctx, `UPDATE schema_migrations SET checksum=$2 WHERE version=$1`, version, checksum); e != nil {
					return e
				}
			}
			continue
		}
		if e != pgx.ErrNoRows {
			return e
		}
		if _, e = tx.Exec(ctx, string(raw)); e != nil {
			return fmt.Errorf("migration %d: %w", version, e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)`, version, checksum); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

// Open is the development/test convenience. Production uses Migrate then OpenExisting.
func Open(ctx context.Context, url, name string, key []byte) (*Store, error) {
	if e := Migrate(ctx, url, name); e != nil {
		return nil, e
	}
	return OpenExisting(ctx, url, name, key)
}
func OpenExisting(ctx context.Context, url, name string, key []byte) (*Store, error) {
	p, e := pool(ctx, url, name)
	if e != nil {
		return nil, e
	}
	var version int
	e = p.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&version)
	if e != nil || version != SchemaVersion {
		p.Close()
		return nil, fmt.Errorf("schema must be migrated to version %s before serving", strconv.Itoa(SchemaVersion))
	}
	s := &Store{Pool: p, SecretKey: key}
	s.providerTransport = s.newProviderTransport()
	s.startWakeups()
	return s, nil
}
func SchemaName(raw string) bool {
	return regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(strings.TrimSpace(raw))
}
