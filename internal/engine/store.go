package engine

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

type DB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Store struct {
	AttachmentsRoot string

	Matcher Matcher

	AllowLocal bool

	Pool      *pgxpool.Pool
	SecretKey []byte
}

func Open(ctx context.Context, url, schemaName string, key []byte) (*Store, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(schemaName) {
		return nil, Bad("Invalid database schema")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 12
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err = p.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+schemaName); err != nil {
		p.Close()
		return nil, err
	}
	p.Close()
	cfg.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	p, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := &Store{Pool: p, SecretKey: key}
	tx, err := p.Begin(ctx)
	if err != nil {
		p.Close()
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, schemaName); err == nil {
		_, err = tx.Exec(ctx, schema)
	}
	if err != nil {
		p.Close()
		return nil, fmt.Errorf("migration: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() { s.Pool.Close() }
func (s *Store) tx(ctx context.Context, a Actor, fn func(pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// One short workspace lock makes commit order and event cursors agree. Network calls never hold it.
	if _, err = tx.Exec(ctx, `SELECT id FROM scopes WHERE id=$1 FOR UPDATE`, a.Scope); err != nil {
		return err
	}
	if a.CredentialID != "" {
		var valid bool
		err = tx.QueryRow(ctx, `SELECT NOT c.revoked AND NOT p.disabled AND (c.expires_at IS NULL OR c.expires_at>now()) FROM credentials c JOIN principals p ON p.id=c.principal_id AND p.scope=c.scope WHERE c.id=$1 AND c.scope=$2`, a.CredentialID, a.Scope).Scan(&valid)
		if err != nil || !valid {
			return Denied()
		}
	}
	if a.Admin && a.CredentialKind == "browser" {
		var admin bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, a.Scope, a.ID).Scan(&admin); e != nil || !admin {
			return Denied()
		}
	}
	if err = fn(tx); err != nil {
		return normalize(err)
	}
	return tx.Commit(ctx)
}
func normalize(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return Missing()
	}
	var e *pgconn.PgError
	if errors.As(err, &e) {
		if e.Code == "23505" {
			return Conflict("A record already exists.")
		}
		if e.Code == "23503" {
			return Bad("A referenced record is unavailable.")
		}
	}
	return err
}
func event(ctx context.Context, db DB, a Actor, kind, conv, recipient string, data any) error {
	var seq int64
	if err := db.QueryRow(ctx, `UPDATE scopes SET sequence=sequence+1 WHERE id=$1 RETURNING sequence`, a.Scope).Scan(&seq); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `INSERT INTO events(scope,sequence,kind,actor_id,conversation_id,recipient_id,data) VALUES($1,$2,$3,$4,$5,$6,$7)`, a.Scope, seq, kind, a.ID, conv, recipient, JSON(data))
	return err
}
func idempotent(ctx context.Context, db DB, a Actor, op, key string, input, output any, fn func() error) error {
	if !ValidText(key, 200) {
		return Bad("Idempotency-Key is required (maximum 200 characters).")
	}
	h := Digest(string(JSON(input)))
	var oldHash string
	var old []byte
	err := db.QueryRow(ctx, `SELECT input_hash,output FROM idempotency WHERE scope=$1 AND principal_id=$2 AND operation=$3 AND key=$4`, a.Scope, a.ID, op, key).Scan(&oldHash, &old)
	if err == nil {
		if oldHash != h {
			return Conflict("The idempotency key was already used with different input.")
		}
		return json.Unmarshal(old, output)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err = fn(); err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO idempotency(scope,principal_id,operation,key,input_hash,output) VALUES($1,$2,$3,$4,$5,$6)`, a.Scope, a.ID, op, key, h, JSON(output))
	return err
}
func (s *Store) Bootstrap(ctx context.Context, scope, company, environment, token string) (Actor, error) {
	if !ValidText(scope, 200) || len(token) < 32 {
		return Actor{}, Bad("Bootstrap token must contain at least 32 characters.")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Actor{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, scope); err != nil {
		return Actor{}, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scopes WHERE id=$1)`, scope).Scan(&exists); err != nil {
		return Actor{}, err
	}
	if !exists {
		if _, err = tx.Exec(ctx, `INSERT INTO scopes(id,company_name,environment) VALUES($1,$2,$3)`, scope, company, environment); err != nil {
			return Actor{}, err
		}
		p := ID("per")
		if _, err = tx.Exec(ctx, `INSERT INTO principals(id,scope,kind,external_id,name) VALUES($1,$2,'person','owner','Local Developer')`, p, scope); err != nil {
			return Actor{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO credentials(id,scope,principal_id,digest,kind) VALUES($1,$2,$3,$4,'admin')`, ID("key"), scope, p, Digest(token)); err != nil {
			return Actor{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Actor{}, err
	}
	return s.Authenticate(ctx, token)
}
func (s *Store) Authenticate(ctx context.Context, token string) (Actor, error) {
	var a Actor
	var meta []byte
	err := s.Pool.QueryRow(ctx, `SELECT p.id,p.kind,p.external_id,p.name,p.metadata,p.disabled,p.created_at,c.scope,c.id,c.kind,c.prefix,COALESCE(i.id,'') FROM credentials c JOIN principals p ON p.id=c.principal_id AND p.scope=c.scope LEFT JOIN instances i ON i.credential_id=c.id AND i.scope=c.scope WHERE c.digest=$1 AND NOT c.revoked AND NOT p.disabled AND (c.expires_at IS NULL OR c.expires_at>now())`, Digest(token)).Scan(&a.ID, &a.Kind, &a.ExternalID, &a.Name, &meta, &a.Disabled, &a.CreatedAt, &a.Scope, &a.CredentialID, &a.CredentialKind, &a.Prefix, &a.InstanceID)
	if err != nil {
		return a, &Error{"UNAUTHENTICATED", "A valid credential is required.", 401}
	}
	_ = json.Unmarshal(meta, &a.Metadata)
	a.Admin = a.CredentialKind == "admin"
	if a.CredentialKind == "browser" {
		_ = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, a.Scope, a.ID).Scan(&a.Admin)
	}
	return a, nil
}
func issue(ctx context.Context, db DB, a Actor, pid, kind, prefix string, ttl time.Duration) (string, string, error) {
	token, id := Secret(), ID("key")
	var expires *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expires = &t
	}
	_, err := db.Exec(ctx, `INSERT INTO credentials(id,scope,principal_id,digest,kind,prefix,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, a.Scope, pid, Digest(token), kind, prefix, expires)
	return token, id, err
}
