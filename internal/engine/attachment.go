package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Attachment struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	FileName       string    `json:"file_name"`
	MediaType      string    `json:"media_type"`
	Size           int64     `json:"size"`
	SHA256         string    `json:"sha256"`
	CreatedAt      time.Time `json:"created_at"`
}

const maxAttachment = 5 << 20

func (s *Store) attachmentPath(scope, id string) string {
	return filepath.Join(s.AttachmentsRoot, Digest(scope), id)
}
func (s *Store) UploadAttachment(ctx context.Context, a Actor, conv, name, media string, data []byte) (Attachment, error) {
	var out Attachment
	if s.AttachmentsRoot == "" && s.AttachmentObjects == nil {
		return out, Bad("Attachment storage is not configured.")
	}
	if len(data) == 0 || len(data) > maxAttachment {
		return out, Bad("Attachments must be between 1 byte and 5 MiB.")
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if !ValidText(name, 200) || name == "." || name == "/" || strings.ContainsAny(name, "\r\n\x00") {
		return out, Bad("Invalid filename.")
	}
	allowed := map[string]bool{"text/plain": true, "application/pdf": true, "application/json": true, "image/png": true, "image/jpeg": true, "image/webp": true}
	if !allowed[media] {
		return out, Bad("Unsupported attachment type.")
	}
	if media == "image/png" && !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return out, Bad("Invalid PNG data.")
	}
	if media == "application/pdf" && !bytes.HasPrefix(data, []byte("%PDF-")) {
		return out, Bad("Invalid PDF data.")
	}
	if _, e := s.conversationRecipients(ctx, s.Pool, a, conv, "message"); e != nil {
		return out, e
	}
	hash := sha256.Sum256(data)
	out = Attachment{ID: ID("file"), ConversationID: conv, FileName: name, MediaType: media, Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:]), CreatedAt: time.Now()}
	path := s.attachmentPath(a.Scope, out.ID)
	// A crash between remote PUT and metadata COMMIT used to leave untracked
	// content. Reserve a cleanup lease before any external object is created.
	if e := s.tx(ctx, a, func(tx pgx.Tx) error {
		if _, e := s.conversationRecipients(ctx, tx, a, conv, "message"); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO attachment_uploads(id,scope) VALUES($1,$2)`, out.ID, a.Scope)
		return e
	}); e != nil {
		return out, e
	}
	if s.AttachmentObjects != nil {
		upload, cancel := context.WithTimeout(ctx, 30*time.Second)
		e := s.AttachmentObjects.Put(upload, attachmentObjectKey(a.Scope, out.ID), data)
		cancel()
		if e != nil {
			return out, fmt.Errorf("attachment upload failed: %w", e)
		}
	} else {
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return out, e
		}
		if e := os.WriteFile(path, data, 0600); e != nil {
			return out, e
		}
	}
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
		if _, e := s.conversationRecipients(ctx, tx, a, conv, "message"); e != nil {
			return e
		}
		var reserved string
		if e := tx.QueryRow(ctx, `DELETE FROM attachment_uploads WHERE scope=$1 AND id=$2 AND lease_until>now() RETURNING id`, a.Scope, out.ID).Scan(&reserved); e != nil {
			return Conflict("Upload reservation expired; retry the upload.")
		}
		if _, e := tx.Exec(ctx, `INSERT INTO attachments(id,scope,conversation_id,owner_id,file_name,media_type,size,sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, out.ID, a.Scope, conv, a.ID, name, media, len(data), out.SHA256); e != nil {
			return e
		}
		return event(ctx, tx, a, "attachment.created", conv, "", out)
	})
	if e != nil {
		// A dropped COMMIT response can mean the metadata was committed. Never
		// remove its content merely because the original connection failed.
		check, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var exists bool
		if err := s.Pool.QueryRow(check, `SELECT EXISTS(SELECT 1 FROM attachments WHERE scope=$1 AND id=$2)`, a.Scope, out.ID).Scan(&exists); err == nil && !exists {
			if s.AttachmentObjects != nil {
				if err := s.AttachmentObjects.Delete(check, attachmentObjectKey(a.Scope, out.ID)); err == nil {
					s.removeUploadReservation(check, a.Scope, out.ID)
				}
			} else {
				if err := os.Remove(path); err == nil || os.IsNotExist(err) {
					s.removeUploadReservation(check, a.Scope, out.ID)
				}
			}
		}
	}
	return out, e
}
func (s *Store) Attachments(ctx context.Context, a Actor, conv string) ([]Attachment, error) {
	if !s.canRead(ctx, s.Pool, a, conv) {
		return nil, Missing()
	}
	rows, e := s.Pool.Query(ctx, `SELECT id,conversation_id,file_name,media_type,size,sha256,created_at FROM attachments WHERE scope=$1 AND conversation_id=$2 ORDER BY created_at`, a.Scope, conv)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		var x Attachment
		if e = rows.Scan(&x.ID, &x.ConversationID, &x.FileName, &x.MediaType, &x.Size, &x.SHA256, &x.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) DownloadAttachment(ctx context.Context, a Actor, id string) (Attachment, io.ReadSeekCloser, error) {
	var x Attachment
	e := s.Pool.QueryRow(ctx, `SELECT id,conversation_id,file_name,media_type,size,sha256,created_at FROM attachments WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&x.ID, &x.ConversationID, &x.FileName, &x.MediaType, &x.Size, &x.SHA256, &x.CreatedAt)
	if e != nil || !s.canRead(ctx, s.Pool, a, x.ConversationID) {
		return x, nil, Missing()
	}
	var file io.ReadSeekCloser
	if s.AttachmentObjects != nil {
		file, e = s.AttachmentObjects.Open(ctx, attachmentObjectKey(a.Scope, id))
	} else {
		file, e = os.Open(s.attachmentPath(a.Scope, id))
	}
	if e != nil {
		return x, nil, fmt.Errorf("attachment content unavailable")
	}
	if s.AttachmentObjects != nil {
		hash := sha256.New()
		n, err := io.Copy(hash, io.LimitReader(file, maxAttachment+1))
		if err != nil || n != x.Size || hex.EncodeToString(hash.Sum(nil)) != x.SHA256 {
			file.Close()
			return x, nil, fmt.Errorf("attachment content integrity check failed")
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			file.Close()
			return x, nil, fmt.Errorf("attachment content unavailable")
		}
	}
	return x, file, nil
}
