package engine

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This child-first order is deliberately checked against the live catalog.
// A future scoped table must be considered before erasure or export can proceed.
var lifecycleTables = []string{"workspace_usage", "workspace_budgets", "answer_match_jobs", "overseer_plans", "answer_proposals", "delivery_attempts", "provider_threads", "participants", "request_audience", "execution_intents", "messages", "attachments", "requests", "shared_answers", "conversations", "deliveries", "instances", "credentials", "destinations", "connections", "memberships", "role_bindings", "roles", "groups", "idempotency", "events", "provider_events", "principals"}

type ErasureStatus struct {
	Operation string     `json:"operation"`
	Due       *time.Time `json:"due"`
	Started   *time.Time `json:"started"`
	Completed *time.Time `json:"completed"`
}

// ProcessWorkspaceErasure is an optional maintenance path with no queued work
// until an operator explicitly reserves erasure. Failures retain all retry state.
func (s *Store) ProcessWorkspaceErasure(ctx context.Context) (bool, error) {
	var scope, operation string
	err := s.Pool.QueryRow(ctx, `SELECT id,erasure_operation FROM scopes WHERE erasure_operation<>'' AND erased_at IS NULL AND erase_after<=now() AND erasure_next_at<=now() ORDER BY erasure_next_at,erase_after LIMIT 1`).Scan(&scope, &operation)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = s.EraseWorkspaceStep(ctx, scope, operation)
	if ErrorStatus(err) == 409 {
		return false, nil
	}
	delay := time.Second
	if err != nil {
		delay = 30 * time.Second
	}
	_, updateErr := s.Pool.Exec(ctx, `UPDATE scopes SET erasure_next_at=now()+$2::interval WHERE id=$1`, scope, delay.String())
	if err == nil {
		err = updateErr
	}
	return true, err
}

func (s *Store) WorkspaceErasure(ctx context.Context, scope string) (ErasureStatus, error) {
	var out ErasureStatus
	err := s.Pool.QueryRow(ctx, `SELECT erasure_operation,erase_after,erasure_started_at,erased_at FROM scopes WHERE id=$1`, scope).Scan(&out.Operation, &out.Due, &out.Started, &out.Completed)
	return out, normalize(err)
}

// ReserveWorkspaceErasure is an operator primitive, never a customer endpoint.
// Reservation immediately suspends access. Retention is an explicit deadline;
// subscription expiry alone never schedules destructive work.
func (s *Store) ReserveWorkspaceErasure(ctx context.Context, scope, operation string, version int64, due time.Time) error {
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(operation) || version < 1 || due.IsZero() {
		return Bad("An explicit erasure operation, version and retention deadline are required.")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var current int64
	var old string
	var previous *time.Time
	if err = tx.QueryRow(ctx, `SELECT control_version,erasure_operation,erase_after FROM scopes WHERE id=$1 FOR UPDATE`, scope).Scan(&current, &old, &previous); err != nil {
		return normalize(err)
	}
	due = due.UTC().Truncate(time.Microsecond)
	if old != "" {
		if old == operation && current == version && previous != nil && previous.Equal(due) {
			return nil
		}
		return Conflict("An erasure operation is already reserved.")
	}
	if version <= current {
		return Conflict("A newer workspace state is already applied.")
	}
	_, err = tx.Exec(ctx, `UPDATE scopes SET suspended=true,control_version=$2,erasure_operation=$3,erase_after=$4,automatic_answers=false,overseer_enabled=false WHERE id=$1`, scope, version, operation, due)
	if err != nil {
		return err
	}
	if err = event(ctx, tx, Actor{Scope: scope}, "workspace.erasure", "", "", map[string]any{"operation": operation, "due": due}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func lifecycleCatalog(ctx context.Context, db DB) error {
	rows, err := db.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid WHERE n.nspname=current_schema() AND c.relkind IN ('r','p') AND a.attname='scope' AND NOT a.attisdropped`)
	if err != nil {
		return err
	}
	defer rows.Close()
	known := map[string]bool{}
	for _, t := range lifecycleTables {
		known[t] = true
	}
	for rows.Next() {
		var table string
		if err = rows.Scan(&table); err != nil {
			return err
		}
		if !known[table] {
			return fmt.Errorf("unrecognized scoped table %q; lifecycle upgrade required", table)
		}
		delete(known, table)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(known) != 0 {
		return fmt.Errorf("incomplete lifecycle schema")
	}
	return nil
}

// Export and erasure coordinate across replicas without locking unrelated
// workspaces. A failed unlock discards the connection instead of pooling a lock.
func (s *Store) lifecycleConnection(ctx context.Context, scope string, shared bool) (*pgxpool.Conn, func(), error) {
	c, err := s.Pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	h := sha256.Sum256([]byte("notif-lifecycle:" + scope))
	key := int64(binary.BigEndian.Uint64(h[:8]))
	lock, unlock := "pg_try_advisory_lock", "pg_advisory_unlock"
	if shared {
		lock += "_shared"
		unlock += "_shared"
	}
	var ok bool
	if err = c.QueryRow(ctx, "SELECT "+lock+"($1)", key).Scan(&ok); err != nil || !ok {
		c.Release()
		if err == nil {
			err = Conflict("Workspace maintenance is already running.")
		}
		return nil, nil, err
	}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		if e := c.QueryRow(cleanup, "SELECT "+unlock+"($1)", key).Scan(&unlocked); e != nil || !unlocked {
			raw := c.Hijack()
			raw.Close(cleanup)
		} else {
			c.Release()
		}
	}
	return c, release, nil
}

// EraseWorkspaceStep deletes at most eight external objects before committing
// their metadata cleanup. Failures leave a durable retryable reservation. The
// final DB transaction deletes only scope-matched rows and retains a tombstone.
func (s *Store) EraseWorkspaceStep(ctx context.Context, scope, operation string) (bool, error) {
	c, release, err := s.lifecycleConnection(ctx, scope, false)
	if err != nil {
		return false, err
	}
	defer release()
	var stored string
	var due, done bool
	if err = c.QueryRow(ctx, `SELECT erasure_operation,COALESCE(erase_after<=now(),false),erased_at IS NOT NULL FROM scopes WHERE id=$1`, scope).Scan(&stored, &due, &done); err != nil {
		return false, normalize(err)
	}
	if stored == "" || operation != stored {
		return false, Conflict("Erasure operation does not match.")
	}
	if done {
		return true, nil
	}
	if !due {
		return false, nil
	}
	if err = lifecycleCatalog(ctx, c); err != nil {
		return false, err
	}
	if _, err = c.Exec(ctx, `UPDATE scopes SET erasure_started_at=COALESCE(erasure_started_at,now()) WHERE id=$1`, scope); err != nil {
		return false, err
	}
	rows, err := c.Query(ctx, `SELECT id FROM attachments WHERE scope=$1 ORDER BY id LIMIT 8`, scope)
	if err != nil {
		return false, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if !regexp.MustCompile(`^file_[a-f0-9]{32}$`).MatchString(id) {
			return false, fmt.Errorf("invalid attachment identity; erasure requires reconciliation")
		}
		if s.AttachmentObjects != nil {
			err = s.AttachmentObjects.Delete(ctx, attachmentObjectKey(scope, id))
		} else if s.AttachmentsRoot != "" {
			err = os.Remove(s.attachmentPath(scope, id))
			if os.IsNotExist(err) {
				err = nil
			}
		} else {
			err = fmt.Errorf("attachment storage is unavailable")
		}
		if err != nil {
			return false, fmt.Errorf("attachment erasure failed; retry required")
		}
		if _, err = c.Exec(ctx, `DELETE FROM attachments WHERE scope=$1 AND id=$2`, scope, id); err != nil {
			return false, err
		}
	}
	if len(ids) > 0 {
		return false, nil
	}
	tx, err := c.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM scopes WHERE id=$1 FOR UPDATE`, scope); err != nil {
		return false, err
	}
	for _, table := range lifecycleTables {
		if _, err = tx.Exec(ctx, "DELETE FROM "+pgx.Identifier{table}.Sanitize()+" WHERE scope=$1", scope); err != nil {
			return false, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE scopes SET company_name='',sequence=0,erased_at=now() WHERE id=$1`, scope); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// ExportWorkspace streams a consistent tenant-only tar archive. Operational
// credentials, idempotency receipts, provider-event receipts and connection
// secrets/config are deliberately omitted. This is a portable customer-data
// export, not an executable backup or a way to clone working credentials.
func (s *Store) ExportWorkspace(ctx context.Context, scope string, output io.Writer) error {
	c, release, err := s.lifecycleConnection(ctx, scope, true)
	if err != nil {
		return err
	}
	defer release()
	tx, err := c.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var company, environment string
	var started bool
	if err = tx.QueryRow(ctx, `SELECT company_name,environment,erasure_started_at IS NOT NULL FROM scopes WHERE id=$1`, scope).Scan(&company, &environment, &started); err != nil {
		return normalize(err)
	}
	if started {
		return Conflict("Erasure has started; no complete export is available.")
	}
	if err = lifecycleCatalog(ctx, tx); err != nil {
		return err
	}
	tarout := tar.NewWriter(output)
	write := func(name string, size int64, reader io.Reader) error {
		if e := tarout.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: size}); e != nil {
			return e
		}
		_, e := io.Copy(tarout, reader)
		return e
	}
	manifest := JSON(map[string]any{"format": "notif-customer-export-v1", "schema_version": SchemaVersion, "scope": scope, "company": company, "environment": environment, "omitted": []string{"credentials", "idempotency", "provider_events", "connection secret and config", "instance credential_id"}})
	if err = write("manifest.json", int64(len(manifest)), bytes.NewReader(manifest)); err != nil {
		return err
	}
	for _, table := range lifecycleTables {
		if table == "credentials" || table == "idempotency" || table == "provider_events" {
			continue
		}
		if err = func() error {
			f, e := os.CreateTemp("", "notif-export-*")
			if e != nil {
				return e
			}
			defer os.Remove(f.Name())
			defer f.Close()
			omit := "'{digest,secret,credential_id}'::text[]"
			if table == "connections" {
				omit = "'{secret,config}'::text[]"
			}
			rows, e := tx.Query(ctx, "SELECT (to_jsonb(t)-"+omit+")::text FROM "+pgx.Identifier{table}.Sanitize()+" t WHERE scope=$1", scope)
			if e != nil {
				return e
			}
			for rows.Next() {
				var record string
				if e = rows.Scan(&record); e != nil {
					rows.Close()
					return e
				}
				if _, e = io.WriteString(f, record+"\n"); e != nil {
					rows.Close()
					return e
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			size, e := f.Seek(0, io.SeekCurrent)
			if e != nil {
				return e
			}
			if _, e = f.Seek(0, 0); e != nil {
				return e
			}
			return write("tables/"+table+".ndjson", size, f)
		}(); err != nil {
			return err
		}
	}
	rows, err := tx.Query(ctx, `SELECT id,size,sha256 FROM attachments WHERE scope=$1 ORDER BY id`, scope)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, hash string
		var size int64
		if err = rows.Scan(&id, &size, &hash); err != nil {
			return err
		}
		if !regexp.MustCompile(`^file_[a-f0-9]{32}$`).MatchString(id) || size < 1 || size > maxAttachment {
			return fmt.Errorf("invalid attachment metadata")
		}
		err = func() error {
			var source io.ReadSeekCloser
			var e error
			if s.AttachmentObjects != nil {
				source, e = s.AttachmentObjects.Open(ctx, attachmentObjectKey(scope, id))
			} else {
				source, e = os.Open(s.attachmentPath(scope, id))
			}
			if e != nil {
				return fmt.Errorf("export attachment unavailable")
			}
			defer source.Close()
			h := sha256.New()
			n, e := io.Copy(h, io.LimitReader(source, maxAttachment+1))
			if e != nil || n != size || hex.EncodeToString(h.Sum(nil)) != hash {
				return fmt.Errorf("export attachment integrity check failed")
			}
			if _, e = source.Seek(0, 0); e != nil {
				return e
			}
			return write("attachments/"+id, size, io.LimitReader(source, size))
		}()
		if err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	complete := []byte(`{"complete":true}`)
	if err = write("complete.json", int64(len(complete)), bytes.NewReader(complete)); err != nil {
		return err
	}
	return tarout.Close()
}
