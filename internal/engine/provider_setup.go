package engine

import (
	"context"
	"net/url"
	"strings"
)

// ProviderSetup is a credential-free, headless equivalent of the guided setup.
// Reading it never contacts a provider or changes a provider registration.
type ProviderSetup struct {
	ConnectionID string               `json:"connection_id"`
	Kind         string               `json:"kind"`
	Version      int64                `json:"version"`
	Mode         string               `json:"mode"`
	Steps        []string             `json:"steps"`
	Callbacks    map[string]string    `json:"callbacks"`
	Manifest     map[string]any       `json:"manifest,omitempty"`
	Diagnostic   ConnectionDiagnostic `json:"diagnostic"`
}

func (s *Store) ConnectionSetup(ctx context.Context, a Actor, id, publicURL string) (ProviderSetup, error) {
	var out ProviderSetup
	diagnostic, err := s.DiagnoseConnection(ctx, a, id)
	if err != nil {
		return out, err
	}
	item, err := s.queryRecord(ctx, a, "connections", id)
	if err != nil {
		return out, err
	}
	c := item.(Connection)
	out, err = BuildProviderSetup(c, publicURL)
	out.Diagnostic = diagnostic
	return out, err
}

func BuildProviderSetup(c Connection, publicURL string) (ProviderSetup, error) {
	out := ProviderSetup{ConnectionID: c.ID, Kind: c.Kind, Version: c.Version, Mode: "manual", Callbacks: map[string]string{}, Steps: []string{}}
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return out, Bad("A valid public HTTP(S) base URL is required.")
	}
	base := strings.TrimRight(publicURL, "/")
	switch c.Kind {
	case "telegram":
		out.Mode = "byo_webhook"
		out.Callbacks["inbound"] = base + "/integrations/telegram/" + url.PathEscape(c.ID)
		out.Steps = []string{"Use a dedicated bot. Save its immutable bot_id and encrypted secret JSON containing bot_token and webhook_secret. The token prefix must match bot_id.", "Inspect getWebhookInfo yourself before registering the callback. If a webhook already exists elsewhere, stop and use a dedicated bot. Beep Grid never calls setWebhook or deletes an existing webhook.", "Register the HTTPS inbound callback with your webhook_secret, allowed_updates [message, callback_query], and drop_pending_updates=false. Do not run getUpdates while this webhook is active.", "Have the Person start the bot. An authorized administrator links immutable bot_id and user_id with POST /api/v1/people/{id}/telegram-account, then adds the matching private user ID as the destination. Usernames and group chats are unsupported.", "Choose an authorized default_agent_id for new inbound chats. Queue a destination test, reply to that exact bot message, and verify Person attribution and canonical history. Plain text is a clarification/message; approvals require the versioned Approve or Deny button or authenticated workspace.", "Disable the installation to reject old callbacks and queued sends. Rotate the provider token/webhook secret separately when needed, then save and repeat qualification. Do not drop pending updates during recovery."}
	case "slack":
		out.Mode = "byo_manifest"
		out.Callbacks["events"] = base + "/integrations/slack/" + url.PathEscape(c.ID) + "/events"
		out.Callbacks["interactivity"] = base + "/integrations/slack/" + url.PathEscape(c.ID)
		out.Manifest = map[string]any{
			"display_information": map[string]any{"name": "Beep Grid", "description": "Communication with your Beep Grid workspace"},
			"features":            map[string]any{"bot_user": map[string]any{"display_name": "Beep Grid", "always_online": false}, "app_home": map[string]any{"messages_tab_enabled": true, "messages_tab_read_only_enabled": false}},
			"oauth_config":        map[string]any{"scopes": map[string]any{"bot": []string{"chat:write", "im:history", "app_mentions:read"}}},
			"settings":            map[string]any{"event_subscriptions": map[string]any{"request_url": out.Callbacks["events"], "bot_events": []string{"message.im", "app_mention"}}, "interactivity": map[string]any{"is_enabled": true, "request_url": out.Callbacks["interactivity"]}, "socket_mode_enabled": false},
		}
		out.Steps = []string{"Use a dedicated Slack test app. Generate this manifest after saving the installation so callbacks contain its stable ID.", "Import the manifest into Slack. Use a reachable HTTPS public URL, install the bot, and save its immutable team_id plus bot_token/signing_secret as the encrypted secret JSON.", "Link the Person with POST /api/v1/people/{id}/slack-account using immutable user_id and team_id. Select an authorized default_agent_id for new inbound conversations.", "Add the Person destination (DM channel ID). Queue POST /api/v1/connections/{id}/test with destination_id and an Idempotency-Key.", "Inspect the delivery, reply in its Slack thread and verify attribution in the workspace. Test a native approval and current permission denial.", "Disable the installation to revoke local callbacks and queued sends. Revoke or uninstall your dedicated app in Slack separately; this API never takes over an existing app."}
	case "webhook":
		out.Callbacks["inbound"] = base + "/integrations/webhook/" + url.PathEscape(c.ID)
		out.Steps = []string{"Save an HTTPS URL and a signing secret of at least 16 characters. Credentials stay server-side.", "Set inbound_sender_id to an authorized Service for inbound messages. Its contact permissions bound every send.", "Verify X-Notif-Timestamp, X-Notif-Signature (HMAC-SHA256 of v1:timestamp:raw-body), and deduplicate X-Notif-Event-ID. Allow at most five minutes of past skew and one minute of future skew.", "Queue a destination test with a stable Idempotency-Key, inspect provider acceptance, then send an attributed signed inbound message. Acceptance does not prove receipt or reply.", "Disable the installation to reject callbacks and cancel pending delivery authority. Unknown sends require reconciliation before retry."}
	case "email":
		out.Steps = []string{"Save smtp_addr (host:port), from and optional username/password. Production requires secure SMTP submission with a verified sender.", "Add the Person email destination. Queue a destination test with a stable Idempotency-Key and inspect its delivery.", "Verify receipt in the designated mailbox and open the authenticated workspace continuation. SMTP acceptance alone does not prove receipt.", "Replies and approvals happen in the workspace. Inbound email and native attachments are unsupported.", "Disable this installation to stop local sends. Rotate the SMTP credential at the provider separately if needed."}
	default:
		return out, Bad("Unsupported provider setup.")
	}
	return out, nil
}
