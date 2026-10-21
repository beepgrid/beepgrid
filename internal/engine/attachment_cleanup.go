package engine

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"os"
	"regexp"
	"time"
)

// ProcessAttachmentCleanup reclaims content left by an upload crash. The upload
// intent is durable before external I/O; final metadata requires a live lease.
// Restored cells and retained erasure work stay outside automatic reclamation.
func (s *Store) ProcessAttachmentCleanup(ctx context.Context) (bool, error) {
	var scope, id string
	err := s.Pool.QueryRow(ctx, `SELECT u.scope,u.id FROM attachment_uploads u JOIN scopes s ON s.id=u.scope
 WHERE u.lease_until<=now() AND u.next_at<=now() AND NOT s.recovery_hold
 AND (s.erasure_operation='' OR s.erase_after<=now()) ORDER BY u.next_at,u.id LIMIT 1`).Scan(&scope, &id)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !regexp.MustCompile(`^file_[a-f0-9]{32}$`).MatchString(id) {
		return false, fmt.Errorf("invalid upload reservation")
	}
	// Serialize with export/erasure, and recheck after acquiring the lifecycle
	// lock. Restore preparation additionally requires an offline isolated cell.
	connection, release, err := s.lifecycleConnection(ctx, scope, false)
	if err != nil {
		return false, err
	}
	defer release()
	var eligible bool
	err = connection.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attachment_uploads u JOIN scopes s ON s.id=u.scope WHERE u.scope=$1 AND u.id=$2 AND u.lease_until<=now() AND u.next_at<=now() AND NOT s.recovery_hold AND (s.erasure_operation='' OR s.erase_after<=now()) AND NOT EXISTS(SELECT 1 FROM attachments a WHERE a.scope=u.scope AND a.id=u.id))`, scope, id).Scan(&eligible)
	if err != nil || !eligible {
		return false, err
	}
	if s.AttachmentObjects != nil {
		err = s.AttachmentObjects.Delete(ctx, attachmentObjectKey(scope, id))
	} else if s.AttachmentsRoot != "" {
		err = os.Remove(s.attachmentPath(scope, id))
		if os.IsNotExist(err) {
			err = nil
		}
	} else {
		return false, fmt.Errorf("attachment backend unavailable for cleanup")
	}
	if err != nil {
		connection.Exec(ctx, `UPDATE attachment_uploads SET next_at=now()+interval '30 seconds' WHERE scope=$1 AND id=$2`, scope, id)
		return true, fmt.Errorf("attachment cleanup will retry")
	}
	_, err = connection.Exec(ctx, `DELETE FROM attachment_uploads WHERE scope=$1 AND id=$2 AND lease_until<=now()`, scope, id)
	return true, err
}

func (s *Store) removeUploadReservation(ctx context.Context, scope, id string) {
	cleanup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s.Pool.Exec(cleanup, `DELETE FROM attachment_uploads WHERE scope=$1 AND id=$2`, scope, id)
}
