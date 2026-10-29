package notif

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

const MaxAttachmentBytes = 5 << 20

type Attachment struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	MessageID      string    `json:"message_id,omitempty"`
	FileName       string    `json:"file_name"`
	MediaType      string    `json:"media_type"`
	Size           int64     `json:"size"`
	SHA256         string    `json:"sha256"`
	CreatedAt      time.Time `json:"created_at"`
}

func (c *Client) Attachments(ctx context.Context, conversationID string) ([]Attachment, error) {
	var out []Attachment
	err := c.Do(ctx, "GET", "/api/v1/conversations/"+url.PathEscape(conversationID)+"/attachments", nil, &out, "")
	return out, err
}

// UploadAttachment never automatically retries a non-idempotent upload. Inspect
// Attachments after an unknown response before creating another object.
func (c *Client) UploadAttachment(ctx context.Context, conversationID, name, media string, data []byte) (Attachment, error) {
	var out Attachment
	if len(data) == 0 || len(data) > MaxAttachmentBytes || strings.ContainsAny(name+media, "\r\n") {
		return out, fmt.Errorf("invalid attachment or size exceeds 5 MiB")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": name}))
	header.Set("Content-Type", media)
	part, err := writer.CreatePart(header)
	if err != nil {
		return out, err
	}
	if _, err = part.Write(data); err != nil {
		return out, err
	}
	if err = writer.Close(); err != nil {
		return out, err
	}
	raw, err := c.fileRequest(ctx, "POST", "/api/v1/conversations/"+url.PathEscape(conversationID)+"/attachments", writer.FormDataContentType(), body.Bytes())
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}
func (c *Client) DownloadAttachment(ctx context.Context, id string) ([]byte, error) {
	return c.fileRequest(ctx, "GET", "/api/v1/attachments/"+url.PathEscape(id), "", nil)
}
func (c *Client) fileRequest(ctx context.Context, method, path, contentType string, body []byte) ([]byte, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil || base.Host == "" || base.User != nil || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, fmt.Errorf("invalid Beep Grid server URL")
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := copyClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, MaxAttachmentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxAttachmentBytes {
		return nil, fmt.Errorf("attachment response exceeds 5 MiB")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var envelope struct {
			Error APIError `json:"error"`
		}
		_ = json.Unmarshal(raw, &envelope)
		envelope.Error.Status = res.StatusCode
		if envelope.Error.Code == "" {
			envelope.Error.Code = "HTTP_ERROR"
			envelope.Error.Message = "Attachment request rejected"
		}
		return nil, &envelope.Error
	}
	return raw, nil
}
