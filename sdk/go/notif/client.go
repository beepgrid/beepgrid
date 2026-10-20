// Package notif connects ordinary applications and agent runtimes to a Notif server.
package notif

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type Person struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	ExternalID string         `json:"external_id"`
	Metadata   map[string]any `json:"metadata"`
}
type Agent struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	ExternalID string         `json:"external_id"`
	Metadata   map[string]any `json:"metadata"`
}
type Session struct {
	Token      string    `json:"token"`
	Agent      Agent     `json:"agent"`
	InstanceID string    `json:"instance_id"`
	ExpiresAt  time.Time `json:"expires_at"`
}
type EnrollInput struct {
	ParentID     string         `json:"parent_id,omitempty"`
	Agent        string         `json:"agent"`
	Name         string         `json:"name"`
	Capabilities []string       `json:"capabilities"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}
type Action struct {
	Name            string         `json:"name"`
	ExecutorID      string         `json:"executor_id"`
	Resource        string         `json:"resource"`
	ResourceVersion string         `json:"resource_version"`
	Arguments       map[string]any `json:"arguments"`
	OperationKey    string         `json:"operation_key"`
}
type AskInput struct {
	Target         Target     `json:"target"`
	ConversationID string     `json:"conversation_id,omitempty"`
	Kind           string     `json:"kind,omitempty"`
	Title          string     `json:"title"`
	Body           string     `json:"body,omitempty"`
	Choices        []string   `json:"choices,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Action         *Action    `json:"action,omitempty"`
	SharedKey      string     `json:"shared_key,omitempty"`
}
type Response struct {
	ResponderInstanceID string `json:"responder_instance_id,omitempty"`
	SourceAnswerID      string `json:"source_answer_id,omitempty"`
	SourceInvalidated   bool   `json:"source_invalidated,omitempty"`
	Outcome             string `json:"outcome"`
	Answer              any    `json:"answer,omitempty"`
	Version             int    `json:"version"`
}
type Request struct {
	RequesterInstanceID string    `json:"requester_instance_id,omitempty"`
	ID                  string    `json:"id"`
	ConversationID      string    `json:"conversation_id"`
	RequesterID         string    `json:"requester_id"`
	Kind                string    `json:"kind"`
	Title               string    `json:"title"`
	Body                string    `json:"body"`
	Choices             []string  `json:"choices"`
	State               string    `json:"state"`
	Version             int       `json:"version"`
	Response            *Response `json:"response,omitempty"`
	Action              *Action   `json:"action,omitempty"`
	ExpiresAt           time.Time `json:"expires_at"`
}
type SendInput struct {
	Target         Target `json:"target"`
	ConversationID string `json:"conversation_id,omitempty"`
	Body           string `json:"body"`
	Title          string `json:"title,omitempty"`
}
type Message struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Body           string `json:"body"`
	SenderID       string `json:"sender_id"`
	InstanceID     string `json:"instance_id,omitempty"`
}
type SendResult struct {
	ConversationID string    `json:"conversation_id"`
	Messages       []Message `json:"messages"`
}
type Delivery struct {
	ID         string `json:"id"`
	Generation int64  `json:"generation"`
	Payload    struct {
		Type           string   `json:"type"`
		ConversationID string   `json:"conversation_id"`
		Message        *Message `json:"message,omitempty"`
		Request        *Request `json:"request,omitempty"`
	} `json:"payload"`
}

func Key() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func New(baseURL, token string) *Client {
	return &Client{strings.TrimRight(baseURL, "/"), token, &http.Client{Timeout: 20 * time.Second}}
}
func (c *Client) Do(ctx context.Context, method, path string, in, out any, key string) error {
	base, e := url.Parse(c.BaseURL)
	if e != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return fmt.Errorf("invalid Notif server URL")
	}
	if !strings.HasPrefix(path, "/api/v1/") {
		return fmt.Errorf("invalid API path")
	}
	var body []byte
	if in != nil {
		body, e = json.Marshal(in)
		if e != nil {
			return e
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, e := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
		if e != nil {
			return e
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		client := c.HTTP
		if client == nil {
			client = http.DefaultClient
		}
		res, e := client.Do(req)
		if e != nil {
			if attempt < 2 && (method == "GET" || key != "") && ctx.Err() == nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
					continue
				}
			}
			return e
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		res.Body.Close()
		if readErr != nil {
			if attempt < 2 && (method == "GET" || key != "") && ctx.Err() == nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
					continue
				}
			}
			return readErr
		}
		if res.StatusCode >= 400 {
			var envelope struct {
				Error APIError `json:"error"`
			}
			_ = json.Unmarshal(raw, &envelope)
			envelope.Error.Status = res.StatusCode
			if envelope.Error.Code == "" {
				envelope.Error.Code = "HTTP_ERROR"
				envelope.Error.Message = fmt.Sprintf("Server returned %d", res.StatusCode)
			}
			return &envelope.Error
		}
		if out != nil {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
	return fmt.Errorf("request attempts exhausted")
}
func (c *Client) Enroll(ctx context.Context, in EnrollInput) (*Client, Session, error) {
	var s Session
	e := c.Do(ctx, "POST", "/api/v1/sessions", in, &s, "")
	return New(c.BaseURL, s.Token), s, e
}
func (c *Client) Send(ctx context.Context, in SendInput, key string) (SendResult, error) {
	var r SendResult
	if key == "" {
		key = Key()
	}
	e := c.Do(ctx, "POST", "/api/v1/messages", in, &r, key)
	return r, e
}
func (c *Client) Ask(ctx context.Context, in AskInput, key string) (Request, error) {
	var r Request
	if key == "" {
		key = Key()
	}
	e := c.Do(ctx, "POST", "/api/v1/requests", in, &r, key)
	return r, e
}
func (c *Client) GetRequest(ctx context.Context, id string) (Request, error) {
	var r Request
	e := c.Do(ctx, "GET", "/api/v1/requests/"+url.PathEscape(id), nil, &r, "")
	return r, e
}
func (c *Client) Respond(ctx context.Context, id string, in Response) (Request, error) {
	var r Request
	e := c.Do(ctx, "POST", "/api/v1/requests/"+url.PathEscape(id)+"/responses", in, &r, "")
	return r, e
}
func (c *Client) Wait(ctx context.Context, id string) (Request, error) {
	for {
		r, e := c.GetRequest(ctx, id)
		if e != nil || r.State != "open" {
			return r, e
		}
		select {
		case <-ctx.Done():
			return Request{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
func (c *Client) Inbox(ctx context.Context) ([]Delivery, error) {
	var r []Delivery
	e := c.Do(ctx, "GET", "/api/v1/worker/inbox", nil, &r, "")
	return r, e
}
func (c *Client) Ack(ctx context.Context, d Delivery) error {
	return c.Do(ctx, "POST", "/api/v1/worker/inbox/"+url.PathEscape(d.ID)+"/ack", map[string]any{"generation": d.Generation}, nil, "")
}
func (c *Client) Heartbeat(ctx context.Context, progress map[string]any) error {
	return c.Do(ctx, "POST", "/api/v1/worker/heartbeat", progress, nil, "")
}
func (c *Client) Run(ctx context.Context, handler func(context.Context, Delivery) error) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		var jobs []Delivery
		e := c.Do(ctx, "GET", "/api/v1/worker/inbox?wait_ms=15000", nil, &jobs, "")
		if e != nil {
			var apiErr *APIError
			if errors.As(e, &apiErr) && apiErr.Status < 500 && apiErr.Status != 429 {
				return e
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
				continue
			}
		}
		for _, d := range jobs {
			if e = handler(ctx, d); e != nil {
				return e
			}
			if e = c.Ack(ctx, d); e != nil {
				return e
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
