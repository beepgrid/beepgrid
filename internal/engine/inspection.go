package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

type DeliveryEvidence struct {
	ContentVisible bool `json:"content_visible"`
	Delivery
	CreatedAt       time.Time  `json:"created_at"`
	NextAt          time.Time  `json:"next_at"`
	LeaseUntil      *time.Time `json:"lease_until,omitempty"`
	ExactInstanceID string     `json:"exact_instance_id,omitempty"`
	OwnerInstanceID string     `json:"owner_instance_id,omitempty"`
	Attempts        QueryPage  `json:"attempt_history"`
}
type DeliveryAttempt struct {
	Generation int64     `json:"generation"`
	Outcome    string    `json:"outcome"`
	Detail     string    `json:"detail"`
	CreatedAt  time.Time `json:"created_at"`
}
type RequestTrace struct {
	Request    Request   `json:"request"`
	Deliveries QueryPage `json:"deliveries"`
	Executions QueryPage `json:"executions"`
	Evidence   string    `json:"evidence"`
}

func (s *Store) RequestTrace(ctx context.Context, a Actor, id, cursor string) (RequestTrace, error) {
	out := RequestTrace{Evidence: "Delivery states describe transport. Execution states are reported by the authenticated executor; neither proves independent external verification."}
	r, e := s.Request(ctx, a, id)
	if e != nil {
		return out, e
	}
	out.Request = r
	out.Deliveries, e = s.Query(ctx, a, QueryInput{Collection: "deliveries", Filters: map[string]string{"request_id": id}, Cursor: cursor})
	if e != nil {
		return out, e
	}
	out.Executions, e = s.Query(ctx, a, QueryInput{Collection: "executions", Filters: map[string]string{"request_id": id}})
	return out, e
}
func (s *Store) deliveryEvidence(ctx context.Context, a Actor, d Delivery) (DeliveryEvidence, error) {
	out := DeliveryEvidence{Delivery: d, Attempts: QueryPage{Items: []any{}}}
	out.ContentVisible = a.Admin
	if !out.ContentVisible {
		var requester string
		if e := s.Pool.QueryRow(ctx, `SELECT requester_id FROM requests WHERE scope=$1 AND id=split_part($2,'/',1)`, a.Scope, d.OperationID).Scan(&requester); e == nil {
			if requester == a.ID {
				out.ContentVisible = true
			} else if d.RecipientID == a.ID {
				_, e = s.request(ctx, s.Pool, a, d.OperationID)
				out.ContentVisible = e == nil
			}
		}
	}
	if !out.ContentVisible {
		if conv, ok := d.Payload["conversation_id"].(string); ok {
			out.ContentVisible = s.canRead(ctx, s.Pool, a, conv)
		}
	}
	if !out.ContentVisible {
		out.Payload = nil
	}

	e := s.Pool.QueryRow(ctx, `SELECT created_at,next_at,lease_until,exact_instance,owner_instance FROM deliveries WHERE scope=$1 AND id=$2`, a.Scope, d.ID).Scan(&out.CreatedAt, &out.NextAt, &out.LeaseUntil, &out.ExactInstanceID, &out.OwnerInstanceID)
	if e != nil {
		return out, normalize(e)
	}
	// Manual retries can grow history beyond automatic attempt limits.
	out.Attempts, e = s.Query(ctx, a, QueryInput{Collection: "delivery_attempts", Filters: map[string]string{"delivery_id": d.ID}, Limit: 10})
	return out, e
}

// Explicitly queued test messages use the same provider dispatcher and current
// contact policy as normal delivery. Configuration checks never contact providers.
func (s *Store) TestConnection(ctx context.Context, a Actor, id, destination, key string) (Delivery, error) {
	var out Delivery
	e := s.tx(ctx, a, func(db pgx.Tx) error {
		return idempotent(ctx, db, a, "connection_test", key, map[string]string{"connection_id": id, "destination_id": destination}, &out, func() error {
			if !s.mayManageConnection(ctx, db, a, id) {
				return Denied()
			}
			var d Destination
			var enabled bool
			if e := db.QueryRow(ctx, `SELECT d.id,d.principal_id,d.address,c.enabled FROM destinations d JOIN connections c ON c.scope=d.scope AND c.id=d.connection_id WHERE d.scope=$1 AND d.id=$2 AND d.connection_id=$3`, a.Scope, destination, id).Scan(&d.ID, &d.PrincipalID, &d.Address, &enabled); e != nil {
				return normalize(e)
			}
			if !enabled {
				return Conflict("The installation is disabled.")
			}
			if !s.mayManageDestination(ctx, db, a, id, d.PrincipalID) {
				return Denied()
			}
			p, e := principal(ctx, db, a.Scope, d.PrincipalID)
			if e != nil {
				return e
			}
			target := Target{p.Kind, p.ID}
			if p.Disabled || !s.allowed(ctx, db, a, "message", target, id, "") {
				return Denied()
			}
			conv, e := newConversation(ctx, db, a, "Connection test", p.ID)
			if e != nil {
				return e
			}
			m, e := insertMessage(ctx, db, a, conv, "Beep Grid connection test.")
			if e != nil {
				return e
			}
			policy := map[string]any{"sender_id": a.ID, "credential_id": a.CredentialID, "provider_connection": a.ProviderConnection, "action": "message", "target": target}
			payload := map[string]any{"type": "message", "message": m, "conversation_id": conv, "_policy": policy, "diagnostic": true}
			out = Delivery{ID: ID("del"), RecipientID: p.ID, OperationID: m.ID, ConnectionID: id, Address: d.Address, State: "queued", ExpiresAt: time.Now().Add(time.Hour), Payload: payload}
			_, e = db.Exec(ctx, `WITH queued AS (INSERT INTO deliveries(id,scope,operation_id,recipient_id,connection_id,address,payload,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id) SELECT pg_notify('notif_provider','') FROM queued`, out.ID, a.Scope, m.ID, p.ID, id, d.Address, JSON(payload), out.ExpiresAt)
			if e != nil {
				return e
			}
			delete(out.Payload, "_policy")
			return event(ctx, db, a, "connection.test_queued", conv, a.ID, map[string]string{"connection_id": id, "delivery_id": out.ID})
		})
	})
	return out, e
}

type ProviderField struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}
type ProviderCapability struct {
	Kind            string          `json:"kind"`
	Fields          []ProviderField `json:"fields"`
	InboundMessages bool            `json:"inbound_messages"`
	NativeApprovals bool            `json:"native_approvals"`
	WebContinuation bool            `json:"web_continuation"`
	Attachments     bool            `json:"attachments"`
	Builtin         bool            `json:"builtin"`
	Directions      []string        `json:"directions"`
	Replies         bool            `json:"replies"`
	FileMode        string          `json:"file_mode"`
	SetupModes      []string        `json:"setup_modes"`
	Identity        string          `json:"identity"`
	Constraints     []string        `json:"constraints"`
}

func ProviderCatalog() []ProviderCapability {
	out := []ProviderCapability{
		{Kind: "native", Builtin: true, InboundMessages: true, NativeApprovals: true, Attachments: true, Fields: []ProviderField{}},
		{Kind: "webhook", Fields: []ProviderField{{"url", true, false}, {"secret", true, true}, {"inbound_sender_id", false, false}}, InboundMessages: true, WebContinuation: true},
		{Kind: "slack", Fields: []ProviderField{{"team_id", true, false}, {"bot_token", true, true}, {"signing_secret", true, true}, {"default_agent_id", false, false}}, InboundMessages: true, NativeApprovals: true, WebContinuation: true},
		{Kind: "email", Fields: []ProviderField{{"smtp_addr", true, false}, {"from", true, false}, {"username", false, false}, {"secret", false, true}}, WebContinuation: true},
	}
	for i := range out {
		p := &out[i]
		p.Directions = []string{"outbound"}
		p.FileMode = "workspace_link"
		p.SetupModes = []string{"manual"}
		p.Identity = "workspace_person"
		p.Constraints = []string{}
		if p.InboundMessages {
			p.Directions = append(p.Directions, "inbound")
			p.Replies = true
		}
		switch p.Kind {
		case "native":
			p.FileMode = "native"
			p.SetupModes = []string{"builtin"}
		case "slack":
			p.SetupModes = []string{"byo_manifest"}
			p.Identity = "admin_linked_user_and_team"
			p.Constraints = []string{"DMs and app mentions only", "No native file upload", "Replies are messages; final decisions use approval buttons or the workspace"}
		case "email":
			p.Constraints = []string{"Outbound SMTP only", "Reply and decide in the authenticated workspace", "No inbound email or native file transfer"}
		case "webhook":
			p.Identity = "bounded_service"
			p.Constraints = []string{"Inbound sender is a configured Service", "Signed payloads do not grant contact or decision authority", "No native file transfer"}
		}
	}
	return out
}

type ConnectionDiagnostic struct {
	ConnectionID       string   `json:"connection_id"`
	Version            int64    `json:"version"`
	Enabled            bool     `json:"enabled"`
	ConfigurationValid bool     `json:"configuration_valid"`
	Issues             []string `json:"issues"`
	ProviderAcceptance string   `json:"provider_acceptance"`
	Sends              bool     `json:"sends"`
	State              string   `json:"state"`
	Qualification      string   `json:"qualification"`
}

func (s *Store) DiagnoseConnection(ctx context.Context, a Actor, id string) (ConnectionDiagnostic, error) {
	out := ConnectionDiagnostic{ConnectionID: id, Issues: []string{}, ProviderAcceptance: "not_tested", Qualification: "not_verified", State: "setup_incomplete"}
	if !s.mayManageConnection(ctx, s.Pool, a, id) {
		return out, Missing()
	}
	item, e := s.queryRecord(ctx, a, "connections", id)
	if e != nil {
		return out, e
	}
	c := item.(Connection)
	out.Version = c.Version
	out.Enabled = c.Enabled
	for _, provider := range ProviderCatalog() {
		if provider.Kind == c.Kind {
			for _, f := range provider.Fields {
				if f.Required && !f.Secret && c.Config[f.Name] == "" {
					out.Issues = append(out.Issues, "Missing required "+f.Name+".")
				}
			}
		}
	}
	var raw []byte
	if e = s.Pool.QueryRow(ctx, `SELECT secret FROM connections WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&raw); e != nil {
		return out, e
	}
	plain, e := s.unseal(raw)
	if e != nil {
		out.Issues = append(out.Issues, "Stored credential is unavailable.")
	} else if c.Kind == "slack" {
		var auth struct {
			BotToken      string `json:"bot_token"`
			SigningSecret string `json:"signing_secret"`
		}
		if json.Unmarshal(plain, &auth) != nil || auth.BotToken == "" || auth.SigningSecret == "" {
			out.Issues = append(out.Issues, "Slack requires both bot token and signing secret.")
		}
	} else if c.Kind == "webhook" && len(plain) < 16 {
		out.Issues = append(out.Issues, "Webhook signing secret is unavailable.")
	} else if c.Kind == "email" && c.Config["username"] != "" && len(plain) == 0 {
		out.Issues = append(out.Issues, "Authenticated SMTP requires a password.")
	}
	out.ConfigurationValid = len(out.Issues) == 0
	if !c.Enabled {
		out.State = "disconnected"
	} else if out.ConfigurationValid {
		out.State = "configured"
	}
	return out, nil
}
