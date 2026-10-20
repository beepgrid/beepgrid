package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Store struct {
	wakes             wakeHub
	eventWakes        wakeHub
	providerWakes     wakeHub
	stopWakeups       context.CancelFunc
	wakeupDone        chan struct{}
	providerTransport http.RoundTripper

	AttachmentsRoot string

	Matcher Matcher

	AllowLocal bool

	Pool      *pgxpool.Pool
	SecretKey []byte
}

func (s *Store) Close() {
	if transport, ok := s.providerTransport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
	if s.stopWakeups != nil {
		s.stopWakeups()
		<-s.wakeupDone
	}
	s.Pool.Close()
}
func (s *Store) tx(ctx context.Context, a Actor, fn func(pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// One short workspace lock makes commit order and event cursors agree. Network calls never hold it.
	var held bool
	if err = tx.QueryRow(ctx, `SELECT recovery_hold FROM scopes WHERE id=$1 FOR UPDATE`, a.Scope).Scan(&held); err != nil {
		return err
	}
	if held {
		return &Error{"RECOVERY_HOLD", "This workspace is paused for restore reconciliation.", 503}
	}
	if a.ProviderConnection != "" {
		var valid bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM connections c JOIN principals p ON p.scope=c.scope WHERE c.scope=$1 AND c.id=$2 AND c.enabled AND p.id=$3 AND NOT p.disabled)`, a.Scope, a.ProviderConnection, a.ID).Scan(&valid); err != nil || !valid {
			return Denied()
		}
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
	_, err := db.Exec(ctx, `WITH inserted AS (INSERT INTO events(scope,sequence,kind,actor_id,conversation_id,recipient_id,data) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING scope) SELECT pg_notify('notif_events',scope) FROM inserted`, a.Scope, seq, kind, a.ID, conv, recipient, JSON(data))
	if err == nil {
		err = notifyWork(ctx, db, a.Scope, recipient)
	}
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
	actor, err := s.Authenticate(ctx, token)
	if err != nil {
		return Actor{}, err
	}
	if actor.Scope != scope || !actor.Admin {
		return Actor{}, Denied()
	}
	return actor, nil
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
