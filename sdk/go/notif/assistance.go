package notif

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

type AssistanceSettings struct {
	AutomaticAnswers   bool  `json:"automatic_answers"`
	Overseer           bool  `json:"overseer"`
	Version            int64 `json:"version"`
	MatcherConfigured  bool  `json:"matcher_configured"`
	OverseerConfigured bool  `json:"overseer_configured"`
}
type AssistanceUpdate struct {
	Version          int64 `json:"version,omitempty"`
	AutomaticAnswers *bool `json:"automatic_answers,omitempty"`
	Overseer         *bool `json:"overseer,omitempty"`
}
type OverseerInput struct {
	PreviousPlanID string  `json:"previous_plan_id,omitempty"`
	Prompt         string  `json:"prompt"`
	Target         *Target `json:"target,omitempty"`
}
type Instance struct {
	ID           string         `json:"id"`
	AgentID      string         `json:"agent_id"`
	Presence     string         `json:"presence"`
	LastSeen     time.Time      `json:"last_seen"`
	Capabilities []string       `json:"capabilities"`
	Progress     map[string]any `json:"progress"`
}
type FleetAgent struct {
	Agent     Agent      `json:"agent"`
	Instances []Instance `json:"instances"`
	Fresh     bool       `json:"fresh"`
}
type OverseerDispatch struct {
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id"`
	RequestID      string `json:"request_id,omitempty"`
}
type OverseerPlan struct {
	PreviousPlanID       string             `json:"previous_plan_id,omitempty"`
	Target               *Target            `json:"target,omitempty"`
	ID                   string             `json:"id"`
	Prompt               string             `json:"prompt"`
	ContextIDs           []string           `json:"context_agent_ids"`
	Operation            string             `json:"operation"`
	Summary              string             `json:"summary"`
	Body                 string             `json:"body,omitempty"`
	Sources              []FleetAgent       `json:"sources"`
	RequiresConfirmation bool               `json:"requires_confirmation"`
	CreatedAt            time.Time          `json:"created_at"`
	ExpiresAt            time.Time          `json:"expires_at"`
	Dispatches           []OverseerDispatch `json:"dispatches"`
	Replies              []Request          `json:"replies"`
	ReplySummary         string             `json:"reply_summary,omitempty"`
	ReplyDigest          string             `json:"reply_digest,omitempty"`
}

func (c *Client) Assistance(ctx context.Context) (AssistanceSettings, error) {
	var out AssistanceSettings
	err := c.Do(ctx, "GET", "/api/v1/assistance/settings", nil, &out, "")
	return out, err
}
func (c *Client) UpdateAssistance(ctx context.Context, in AssistanceUpdate) (AssistanceSettings, error) {
	var out AssistanceSettings
	err := c.Do(ctx, "POST", "/api/v1/assistance/settings", in, &out, "")
	return out, err
}
func (c *Client) PlanOverseer(ctx context.Context, in OverseerInput, keys ...string) (OverseerPlan, error) {
	var out OverseerPlan
	err := c.assistanceDo(ctx, "/api/v1/fleet/plan", in, &out, keys...)
	return out, err
}
func (c *Client) OverseerPlans(ctx context.Context) ([]OverseerPlan, error) {
	var out []OverseerPlan
	err := c.Do(ctx, "GET", "/api/v1/fleet/plans", nil, &out, "")
	return out, err
}
func (c *Client) OverseerPlan(ctx context.Context, id string) (OverseerPlan, error) {
	var out OverseerPlan
	err := c.Do(ctx, "GET", "/api/v1/fleet/plans/"+url.PathEscape(id), nil, &out, "")
	return out, err
}
func (c *Client) ConfirmOverseer(ctx context.Context, id string) (OverseerPlan, error) {
	var out OverseerPlan
	err := c.Do(ctx, "POST", "/api/v1/fleet/command", map[string]string{"plan_id": id}, &out, "")
	return out, err
}
func (c *Client) SummarizeOverseer(ctx context.Context, id string) (OverseerPlan, error) {
	var out OverseerPlan
	err := c.assistanceDo(ctx, "/api/v1/fleet/plans/"+url.PathEscape(id)+"/summary", struct{}{}, &out)
	return out, err
}

func (c *Client) assistanceDo(ctx context.Context, path string, in, out any, keys ...string) error {
	timeout := c.AssistanceTimeout
	if timeout <= 0 {
		timeout = 40 * time.Second
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	copyClient := *c
	transportClient := http.DefaultClient
	if c.HTTP != nil {
		transportClient = c.HTTP
	}
	copyHTTP := *transportClient
	copyHTTP.Timeout = timeout
	copyClient.HTTP = &copyHTTP
	key := ""
	if len(keys) > 0 {
		key = keys[0]
	}
	return copyClient.Do(bounded, "POST", path, in, out, key)
}
