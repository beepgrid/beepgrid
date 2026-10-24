package notif

import (
	"context"
	"encoding/json"
	"net/url"
	"time"
)

type QueryInput struct {
	Collection string            `json:"collection"`
	Search     string            `json:"search,omitempty"`
	Filters    map[string]string `json:"filters,omitempty"`
	Limit      int               `json:"limit,omitempty"`
	Cursor     string            `json:"cursor,omitempty"`
}
type QueryPage struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
	Order      string            `json:"order"`
}
type CapabilityInput struct {
	Target       *Target `json:"target,omitempty"`
	RequestID    string  `json:"request_id,omitempty"`
	ConnectionID string  `json:"connection_id,omitempty"`
	Descriptor   *Action `json:"descriptor,omitempty"`
}
type CapabilityResult struct {
	ObservedAt     time.Time       `json:"observed_at"`
	Operations     map[string]bool `json:"operations"`
	RequestVersion int             `json:"request_version,omitempty"`
	Outcomes       []string        `json:"outcomes"`
}
type DispatchPreviewInput struct {
	Targets    []Target `json:"targets"`
	Operation  string   `json:"operation"`
	Descriptor *Action  `json:"descriptor,omitempty"`
}
type DispatchPreview struct {
	Recipients       []json.RawMessage `json:"recipients"`
	Count            int               `json:"count"`
	HiddenRecipients int               `json:"hidden_recipients"`
	Token            string            `json:"token"`
	ExpiresAt        time.Time         `json:"expires_at"`
	Sends            bool              `json:"sends"`
}
type SendBatchInput struct {
	Targets      []Target `json:"targets"`
	Body         string   `json:"body"`
	Title        string   `json:"title,omitempty"`
	PreviewToken string   `json:"preview_token,omitempty"`
}
type SendBatchItem struct {
	Target Target     `json:"target"`
	Result SendResult `json:"result"`
}
type AskBatchInput struct {
	Targets      []Target `json:"targets"`
	Request      AskInput `json:"request"`
	PreviewToken string   `json:"preview_token,omitempty"`
}
type AskBatchItem struct {
	Target  Target  `json:"target"`
	Request Request `json:"request"`
}

func (c *Client) Query(ctx context.Context, in QueryInput) (QueryPage, error) {
	var out QueryPage
	e := c.Do(ctx, "POST", "/api/v1/query", in, &out, "")
	return out, e
}
func (c *Client) Capabilities(ctx context.Context, in CapabilityInput) (CapabilityResult, error) {
	var out CapabilityResult
	e := c.Do(ctx, "POST", "/api/v1/capabilities", in, &out, "")
	return out, e
}
func (c *Client) DispatchPreview(ctx context.Context, in DispatchPreviewInput) (DispatchPreview, error) {
	var out DispatchPreview
	e := c.Do(ctx, "POST", "/api/v1/dispatch-preview", in, &out, "")
	return out, e
}
func (c *Client) SendBatch(ctx context.Context, in SendBatchInput, key string) ([]SendBatchItem, error) {
	var out []SendBatchItem
	e := c.Do(ctx, "POST", "/api/v1/message-batches", in, &out, key)
	return out, e
}
func (c *Client) AskBatch(ctx context.Context, in AskBatchInput, key string) ([]AskBatchItem, error) {
	var out []AskBatchItem
	e := c.Do(ctx, "POST", "/api/v1/request-batches", in, &out, key)
	return out, e
}
func (c *Client) Access(ctx context.Context, subject string) (json.RawMessage, error) {
	var out json.RawMessage
	e := c.Do(ctx, "GET", "/api/v1/access?subject_id="+url.QueryEscape(subject), nil, &out, "")
	return out, e
}
func (c *Client) RequestTrace(ctx context.Context, id, cursor string) (json.RawMessage, error) {
	var out json.RawMessage
	e := c.Do(ctx, "GET", "/api/v1/requests/"+url.PathEscape(id)+"/trace?delivery_cursor="+url.QueryEscape(cursor), nil, &out, "")
	return out, e
}
func (c *Client) Workspace(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	e := c.Do(ctx, "GET", "/api/v1/workspace", nil, &out, "")
	return out, e
}
func (c *Client) Account(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	e := c.Do(ctx, "GET", "/api/v1/workspace/account", nil, &out, "")
	return out, e
}
