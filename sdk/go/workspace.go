package beepgrid

import (
	"context"
	"encoding/json"
	"fmt"
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
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
	Targets       []Target `json:"targets"`
	Body          string   `json:"body"`
	Title         string   `json:"title,omitempty"`
	PreviewToken  string   `json:"preview_token,omitempty"`
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

// Mentions address existing participants. Offsets count Unicode code points,
// and never narrow the shared audience or grant contact/decision authority.
type StartChatInput struct {
	AttachmentIDs []string  `json:"attachment_ids,omitempty"`
	Targets       []Target  `json:"targets"`
	Body          string    `json:"body"`
	Title         string    `json:"title,omitempty"`
	Approval      bool      `json:"approval,omitempty"`
	Choices       []string  `json:"choices,omitempty"`
	Mentions      []Mention `json:"mentions,omitempty"`
}

func (c *Client) StartChat(ctx context.Context, in StartChatInput, key string) (SendResult, error) {
	if key == "" {
		key = Key()
	}
	var out SendResult
	err := c.Do(ctx, "POST", "/api/v1/conversations", in, &out, key)
	return out, err
}

// Membership controls disclosure, never IAM or the original request audience.
// ShareHistory is false unless the caller explicitly opts in for additions.
type ChatMembershipInput struct {
	Operation    string   `json:"operation"`
	Version      int      `json:"version"`
	Title        string   `json:"title,omitempty"`
	Members      []string `json:"members,omitempty"`
	ShareHistory bool     `json:"share_history,omitempty"`
	NextManager  string   `json:"next_manager,omitempty"`
}
type ChatMembershipResult struct {
	ConversationID string `json:"conversation_id"`
	Version        int    `json:"version"`
	Left           bool   `json:"left,omitempty"`
}

func (c *Client) ChangeChatMembership(ctx context.Context, conversationID string, in ChatMembershipInput, key string) (ChatMembershipResult, error) {
	if key == "" {
		key = Key()
	}
	var out ChatMembershipResult
	err := c.Do(ctx, "POST", "/api/v1/conversations/"+url.PathEscape(conversationID)+"/membership", in, &out, key)
	return out, err
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

// Directory keeps people, agents and services explicit and permission filtered.
func (c *Client) Directory(ctx context.Context, kind, search string) (QueryPage, error) {
	if kind != "person" && kind != "agent" && kind != "service" {
		return QueryPage{}, fmt.Errorf("invalid identity kind")
	}
	collection := kind + "s"
	if kind == "person" {
		collection = "people"
	}
	return c.Query(ctx, QueryInput{Collection: collection, Search: search})
}
func (c *Client) Conversation(ctx context.Context, id string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.Do(ctx, "GET", "/api/v1/conversations/"+url.PathEscape(id), nil, &out, "")
	return out, err
}
func (c *Client) ProviderCatalog(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.Do(ctx, "GET", "/api/v1/integrations/catalog", nil, &out, "")
	return out, err
}
func (c *Client) ConnectionSetup(ctx context.Context, id string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.Do(ctx, "GET", "/api/v1/connections/"+url.PathEscape(id)+"/setup", nil, &out, "")
	return out, err
}
func (c *Client) Cancel(ctx context.Context, id string, version int) (Request, error) {
	var out Request
	err := c.Do(ctx, "POST", "/api/v1/requests/"+url.PathEscape(id)+"/cancel", map[string]int{"version": version}, &out, "")
	return out, err
}

// ReplaceRequest retires an own pending request and sends a fresh decision to
// its unchanged audience. Keep the key stable when retrying an unknown result.
type ReplaceRequestInput struct {
	Version  int       `json:"version"`
	Body     string    `json:"body"`
	Mentions []Mention `json:"mentions,omitempty"`
}
type ReplacedRequest struct {
	Previous    Request `json:"previous"`
	Replacement Request `json:"replacement"`
}

func (c *Client) ReplaceRequest(ctx context.Context, id string, in ReplaceRequestInput, key string) (ReplacedRequest, error) {
	var out ReplacedRequest
	err := c.Do(ctx, "POST", "/api/v1/requests/"+url.PathEscape(id)+"/replace", in, &out, key)
	return out, err
}
