// Package beepgrid connects ordinary applications and agent runtimes to a Beep Grid server.
package beepgrid

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
	// AssistanceTimeout applies only to model-backed planning and summaries.
	// Zero uses 40 seconds; a shorter caller context still takes precedence.
	AssistanceTimeout time.Duration
	// MaxResponseBytes bounds decoded responses. Zero uses 256 MiB, covering the
	// API's bounded 1000-recipient fan-out even with JSON escaping. Decoding streams.
	MaxResponseBytes int64
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
	Token      string `json:"token"`
	Agent      Agent  `json:"agent"`
	InstanceID string `json:"instance_id"`
	// ExpiresAt is zero when the connection never expires.
	ExpiresAt time.Time `json:"expires_at"`
}
type EnrollInput struct {
	NeverExpires   bool           `json:"never_expires,omitempty"`
	ConnectionName string         `json:"connection_name,omitempty"`
	Renewable      bool           `json:"renewable,omitempty"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
	ParentID       string         `json:"parent_id,omitempty"`
	Agent          string         `json:"agent"`
	Name           string         `json:"name"`
	Capabilities   []string       `json:"capabilities"`
	Metadata       map[string]any `json:"metadata,omitempty"`
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
	AttachmentIDs  []string   `json:"attachment_ids,omitempty"`
	Mentions       []Mention  `json:"mentions,omitempty"`
	Blocking       bool       `json:"blocking,omitempty"`
	ReuseDirect    bool       `json:"reuse_direct,omitempty"`
	PreviewToken   string     `json:"preview_token,omitempty"`
	Target         Target     `json:"target"`
	ConversationID string     `json:"conversation_id,omitempty"`
	Kind           string     `json:"kind,omitempty"`
	Title          string     `json:"title,omitempty"`
	Body           string     `json:"body,omitempty"`
	Choices        []string   `json:"choices,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Action         *Action    `json:"action,omitempty"`
	SharedKey      string     `json:"shared_key,omitempty"`
}
type Response struct {
	ResponderInstanceID string `json:"responder_instance_id,omitempty"`
	SourceAnswerID      string `json:"source_answer_id,omitempty"`
	ReuseMethod         string `json:"reuse_method,omitempty"`
	ReuseReason         string `json:"reuse_reason,omitempty"`
	SourceInvalidated   bool   `json:"source_invalidated,omitempty"`
	Outcome             string `json:"outcome"`
	Answer              any    `json:"answer,omitempty"`
	Version             int    `json:"version"`
}
type Request struct {
	Attachments         []Attachment `json:"attachments,omitempty"`
	Mentions            []Mention    `json:"mentions,omitempty"`
	Blocking            bool         `json:"blocking,omitempty"`
	RequesterInstanceID string       `json:"requester_instance_id,omitempty"`
	ID                  string       `json:"id"`
	ConversationID      string       `json:"conversation_id"`
	RequesterID         string       `json:"requester_id"`
	Kind                string       `json:"kind"`
	Title               string       `json:"title"`
	Body                string       `json:"body"`
	Choices             []string     `json:"choices"`
	State               string       `json:"state"`
	Version             int          `json:"version"`
	Response            *Response    `json:"response,omitempty"`
	Action              *Action      `json:"action,omitempty"`
	ExpiresAt           time.Time    `json:"expires_at"`
}
type Mention struct {
	PrincipalID string `json:"principal_id"`
	Start       int    `json:"start"`
	End         int    `json:"end"`
}
type SendInput struct {
	AttachmentIDs  []string  `json:"attachment_ids,omitempty"`
	Mentions       []Mention `json:"mentions,omitempty"`
	ReuseDirect    bool      `json:"reuse_direct,omitempty"`
	PreviewToken   string    `json:"preview_token,omitempty"`
	Target         Target    `json:"target"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Body           string    `json:"body"`
	Title          string    `json:"title,omitempty"`
}
type Message struct {
	ReceivedBy     []string     `json:"received_by,omitempty"`
	Attachments    []Attachment `json:"attachments,omitempty"`
	Mentions       []Mention    `json:"mentions,omitempty"`
	ID             string       `json:"id"`
	ConversationID string       `json:"conversation_id"`
	Body           string       `json:"body"`
	SenderID       string       `json:"sender_id"`
	InstanceID     string       `json:"instance_id,omitempty"`
}
type SendResult struct {
	ConversationID string    `json:"conversation_id"`
	Messages       []Message `json:"messages"`
}

// Execution is an authorization intent and worker report, not proof of an external effect.
type Execution struct {
	ID           string `json:"id"`
	RequestID    string `json:"request_id"`
	ExecutorID   string `json:"executor_id"`
	OperationKey string `json:"operation_key"`
	Action       Action `json:"action"`
	State        string `json:"state"`
	Result       any    `json:"result"`
}
type Delivery struct {
	ID         string `json:"id"`
	Generation int64  `json:"generation"`
	Payload    struct {
		AnycastID      string   `json:"anycast_id,omitempty"`
		AnycastRoleID  string   `json:"anycast_role_id,omitempty"`
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
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Do(ctx context.Context, method, path string, in, out any, key string) error {
	return c.do(ctx, method, path, in, out, key, false)
}
func (c *Client) do(ctx context.Context, method, path string, in, out any, key string, minimal bool) error {
	base, e := url.Parse(c.BaseURL)
	if e != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return fmt.Errorf("invalid Beep Grid server URL")
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
		if minimal {
			req.Header.Set("Prefer", "return=minimal")
		}
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
		limit := c.MaxResponseBytes
		if limit <= 0 {
			limit = 256 << 20
		}
		limited := &io.LimitedReader{R: res.Body, N: limit + 1}
		decoder := json.NewDecoder(limited)
		var envelope struct {
			Error APIError `json:"error"`
		}
		var readErr error
		if res.StatusCode >= 400 {
			readErr = decoder.Decode(&envelope)
		} else if out != nil {
			readErr = decoder.Decode(out)
		} else {
			_, readErr = io.Copy(io.Discard, limited)
		}
		if readErr == nil && out != nil && res.StatusCode < 400 {
			var trailing any
			if e := decoder.Decode(&trailing); e != io.EOF {
				if e == nil {
					e = fmt.Errorf("multiple JSON response values")
				}
				readErr = e
			}
		}
		res.Body.Close()
		if limited.N <= 0 {
			return fmt.Errorf("Beep Grid response exceeds %d byte limit", limit)
		}
		// Error pages need not be JSON to preserve the useful HTTP status.
		if res.StatusCode >= 400 {
			envelope.Error.Status = res.StatusCode
			if envelope.Error.Code == "" {
				envelope.Error.Code = "HTTP_ERROR"
				envelope.Error.Message = fmt.Sprintf("Server returned %d", res.StatusCode)
			}
			return &envelope.Error
		}
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

// SendReceipt confirms durable acceptance, not delivery or execution.
type SendReceipt struct {
	ConversationID string `json:"conversation_id"`
	MessageCount   int    `json:"message_count"`
}

// SendMinimal avoids retransmitting canonical messages the caller already sent.
// Send with the same input and key recovers the complete receipt without a resend.
func (c *Client) SendMinimal(ctx context.Context, in SendInput, key string) (SendReceipt, error) {
	var r SendReceipt
	if key == "" {
		key = Key()
	}
	err := c.do(ctx, "POST", "/api/v1/messages", in, &r, key, true)
	return r, err
}
func (c *Client) Ask(ctx context.Context, in AskInput, key string) (Request, error) {
	var r Request
	if key == "" {
		key = Key()
	}
	e := c.Do(ctx, "POST", "/api/v1/requests", in, &r, key)
	return r, e
}

// SetRequestBlocking declares whether this agent needs the answer to continue.
// It does not cancel work or infer a dependency from an ordinary open request.
func (c *Client) SetRequestBlocking(ctx context.Context, id string, blocking bool) (Request, error) {
	var out Request
	err := c.Do(ctx, "POST", "/api/v1/requests/"+url.PathEscape(id)+"/blocking", map[string]bool{"blocking": blocking}, &out, "")
	return out, err
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
		if len(jobs) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
}

// Redeem rechecks current authority and returns the same intent for repeated calls.
// A protected business tool must still enforce OperationKey and ResourceVersion.
func (c *Client) Redeem(ctx context.Context, id string, action Action) (Execution, error) {
	var out Execution
	err := c.Do(ctx, "POST", "/api/v1/requests/"+url.PathEscape(id)+"/redeem", action, &out, Key())
	return out, err
}
func (c *Client) ReportExecution(ctx context.Context, id, state string, result any) (Execution, error) {
	var out Execution
	err := c.Do(ctx, "POST", "/api/v1/executions/"+url.PathEscape(id), map[string]any{"state": state, "result": result}, &out, Key())
	return out, err
}

// AttentionFor is an addressing hint, never an exclusive responder rule.
func (m Message) AttentionFor(recipient string) string {
	if len(m.Mentions) == 0 {
		return ""
	}
	for _, mention := range m.Mentions {
		if mention.PrincipalID == recipient {
			return "You are explicitly addressed. Other participants may contribute relevant context."
		}
	}
	return "Other participants are explicitly addressed. You may contribute relevant context."
}
