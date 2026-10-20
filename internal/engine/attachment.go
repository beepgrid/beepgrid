package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5"
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
	if s.AttachmentsRoot == "" {
		return out, Bad("Attachment storage is not configured.")
	}
	if len(data) == 0 || len(data) > maxAttachment {
		return out, Bad("Attachments must be between 1 byte and 5 MiB.")
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if !ValidText(name, 200) || strings.ContainsAny(name, "\r\n\x00") {
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
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return out, e
	}
	if e := os.WriteFile(path, data, 0600); e != nil {
		return out, e
	}
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
		if _, e := s.conversationRecipients(ctx, tx, a, conv, "message"); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO attachments(id,scope,conversation_id,owner_id,file_name,media_type,size,sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, out.ID, a.Scope, conv, a.ID, name, media, len(data), out.SHA256); e != nil {
			return e
		}
		return event(ctx, tx, a, "attachment.created", conv, "", out)
	})
	if e != nil {
		_ = os.Remove(path)
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
func (s *Store) DownloadAttachment(ctx context.Context, a Actor, id string) (Attachment, *os.File, error) {
	var x Attachment
	e := s.Pool.QueryRow(ctx, `SELECT id,conversation_id,file_name,media_type,size,sha256,created_at FROM attachments WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&x.ID, &x.ConversationID, &x.FileName, &x.MediaType, &x.Size, &x.SHA256, &x.CreatedAt)
	if e != nil || !s.canRead(ctx, s.Pool, a, x.ConversationID) {
		return x, nil, Missing()
	}
	file, e := os.Open(s.attachmentPath(a.Scope, id))
	if e != nil {
		return x, nil, fmt.Errorf("attachment content unavailable")
	}
	return x, file, nil
}
