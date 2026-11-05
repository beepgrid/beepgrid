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
	Registration map[string]any       `json:"registration,omitempty"`
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
	case "discord":
		out.Mode = "byo_interactions"
		out.Callbacks["interactions"] = base + "/integrations/discord/" + url.PathEscape(c.ID)
		out.Registration = map[string]any{
			"installation_url": "https://discord.com/oauth2/authorize?" + url.Values{"client_id": {c.Config["application_id"]}, "scope": {"bot applications.commands"}, "permissions": {"0"}, "integration_type": {"0"}}.Encode(),
			"command_endpoint": "https://discord.com/api/v10/applications/" + url.PathEscape(c.Config["application_id"]) + "/commands",
			"command":          map[string]any{"name": "beepgrid", "description": "Send a message to your BeepGrid agent", "type": 1, "contexts": []int{1}, "integration_types": []int{0}, "options": []any{map[string]any{"name": "message", "description": "Message text", "type": 3, "required": true, "max_length": 2000}}},
		}
		out.Steps = []string{"Create a dedicated Discord application. Save its immutable application_id, hexadecimal public_key and encrypted secret JSON containing bot_token. Never use a personal account token.", "Set the exact HTTPS Interactions Endpoint URL. Discord's signed PING must succeed and invalid signatures must fail. Keep all privileged Gateway intents disabled.", "Register only the supplied /beepgrid command on this dedicated application. The setup endpoint never registers commands or changes an existing app.", "Install the bot and applications.commands scopes in an authorized test server using the supplied URL with no guild permission bits. The first release accepts interactions in the bot DM only, not server or other private-channel contexts.", "Have the Person open the bot DM and initiate the interaction. An authorized identity administrator links immutable application_id and user_id. Add the exact linked user ID as a destination, then grant the required contact and decision authority separately.", "Queue a destination test, use Reply to submit its form and inspect the attributed canonical reply. Ordinary DM text is not ingested because this adapter has no Gateway listener. Test approval buttons separately.", "Disable the connection to block callbacks and queued sends. Revoke or uninstall the dedicated app in Discord separately. Rate limits can retry; unknown message outcomes require reconciliation."}
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
			"oauth_config":        map[string]any{"scopes": map[string]any{"bot": []string{"chat:write", "im:history", "app_mentions:read", "channels:read", "groups:read", "channels:history", "groups:history"}}},
			"settings":            map[string]any{"event_subscriptions": map[string]any{"request_url": out.Callbacks["events"], "bot_events": []string{"message.im", "app_mention", "message.channels", "message.groups"}}, "interactivity": map[string]any{"is_enabled": true, "request_url": out.Callbacks["interactivity"]}, "socket_mode_enabled": false},
		}
		out.Steps = []string{"Create a dedicated Slack app and save its signing_secret plus immutable team_id in a setup connection. Keep it enabled for signed URL verification; add bot_token after installation. Generate this manifest for its stable connection ID.", "Import the manifest into that dedicated app, verify its reachable HTTPS callbacks, install the bot, then save bot_token and signing_secret as the encrypted secret JSON.", "Link the Person with POST /api/v1/people/{id}/slack-account using immutable user_id and team_id. Select an authorized default_agent_id for new inbound conversations.", "Update and reinstall existing apps for channel-list and channel-history scopes. Invite the bot to the chosen channels, configure channel access, then add the Person destination (Slack channel or DM ID). Queue POST /api/v1/connections/{id}/test with destination_id and an Idempotency-Key.", "Inspect the delivery, reply in its Slack thread and verify attribution in the workspace. Test a native approval and current permission denial.", "Disable the installation to revoke local callbacks and queued sends. Revoke or uninstall your dedicated app in Slack separately; this API never takes over an existing app."}
	case "webhook":
		out.Callbacks["inbound"] = base + "/integrations/webhook/" + url.PathEscape(c.ID)
		out.Steps = []string{"Save an HTTPS URL and a signing secret of at least 16 characters. Credentials stay server-side.", "Set inbound_sender_id to an authorized Service for inbound messages. Its contact permissions bound every send.", "Verify X-Beepgrid-Timestamp, X-Beepgrid-Signature (HMAC-SHA256 of v1:timestamp:raw-body), and deduplicate X-Beepgrid-Event-ID. Allow at most five minutes of past skew and one minute of future skew.", "Queue a destination test with a stable Idempotency-Key, inspect provider acceptance, then send an attributed signed inbound message. Acceptance does not prove receipt or reply.", "Disable the installation to reject callbacks and cancel pending delivery authority. Unknown sends require reconciliation before retry."}
	case "email":
		out.Steps = []string{"Save smtp_addr (host:port), from and optional username/password. Production requires secure SMTP submission with a verified sender.", "Add the Person email destination. Queue a destination test with a stable Idempotency-Key and inspect its delivery.", "Verify receipt in the designated mailbox and open the authenticated workspace continuation. SMTP acceptance alone does not prove receipt.", "Replies and approvals happen in the workspace. Inbound email and native attachments are unsupported.", "Disable this installation to stop local sends. Rotate the SMTP credential at the provider separately if needed."}
	default:
		return out, Bad("Unsupported provider setup.")
	}
	return out, nil
}
