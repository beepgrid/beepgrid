package engine

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var discordIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var discordTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{20,256}$`)

func discordID(id string) bool { return discordIDPattern.MatchString(id) }

func parseDiscordSecret(raw string) (string, error) {
	var auth struct {
		BotToken string `json:"bot_token"`
	}
	if json.Unmarshal([]byte(raw), &auth) != nil || !discordTokenPattern.MatchString(auth.BotToken) {
		return "", Bad("Discord requires a bounded bot_token in secret JSON.")
	}
	return auth.BotToken, nil
}

func validDiscordConfig(config map[string]string) bool {
	key, err := hex.DecodeString(config["public_key"])
	return err == nil && len(key) == ed25519.PublicKeySize && discordID(config["application_id"])
}

// Discord signs the unmodified timestamp followed by the raw HTTP body.
func VerifyDiscord(publicKey, timestamp, signature string, body []byte, now time.Time) bool {
	key, err := hex.DecodeString(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize || len(timestamp) > 20 {
		return false
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || seconds < now.Add(-5*time.Minute).Unix() || seconds > now.Add(time.Minute).Unix() {
		return false
	}
	return ed25519.Verify(key, append([]byte(timestamp), body...), sig)
}

// Linking attests an immutable app/user pair and grants no communication rights.
func (s *Store) LinkDiscordAccount(ctx context.Context, a Actor, personID, applicationID, userID string) error {
	if !discordID(applicationID) || !discordID(userID) {
		return Bad("Immutable Discord application_id and user_id are required.")
	}
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.mayLinkAccount(ctx, tx, a, personID) {
			return Denied()
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals WHERE scope=$1 AND kind='person' AND metadata->>'discord_application_id'=$2 AND metadata->>'discord_user_id'=$3 AND id<>$4)`, a.Scope, applicationID, userID, personID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return Conflict("This Discord account is already linked.")
		}
		tag, err := tx.Exec(ctx, `UPDATE principals SET metadata=metadata||$3::jsonb WHERE scope=$1 AND id=$2 AND kind='person'`, a.Scope, personID, JSON(map[string]string{"discord_application_id": applicationID, "discord_user_id": userID}))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return Missing()
		}
		return event(ctx, tx, a, "person.account_linked", "", personID, map[string]string{"provider": "discord", "verified_by": "workspace_admin"})
	})
}

func (s *Store) UnlinkDiscordAccount(ctx context.Context, a Actor, personID string) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.mayLinkAccount(ctx, tx, a, personID) {
			return Denied()
		}
		tag, err := tx.Exec(ctx, `UPDATE principals SET metadata=metadata-'discord_application_id'-'discord_user_id' WHERE scope=$1 AND id=$2 AND kind='person'`, a.Scope, personID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return Missing()
		}
		return event(ctx, tx, a, "person.account_unlinked", "", personID, map[string]string{"provider": "discord"})
	})
}

func (s *Store) discordDestination(ctx context.Context, db DB, scope, connection, person, address string) bool {
	if !discordID(address) {
		return false
	}
	var valid bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals p JOIN connections c ON c.scope=p.scope WHERE p.scope=$1 AND p.id=$2 AND p.kind='person' AND NOT p.disabled AND c.id=$3 AND c.kind='discord' AND p.metadata->>'discord_user_id'=$4 AND p.metadata->>'discord_application_id'=c.config->>'application_id')`, scope, person, connection, address).Scan(&valid)
	return err == nil && valid
}

type DiscordComponent struct {
	Type       int                `json:"type"`
	CustomID   string             `json:"custom_id,omitempty"`
	Value      string             `json:"value,omitempty"`
	Components []DiscordComponent `json:"components,omitempty"`
	Component  *DiscordComponent  `json:"component,omitempty"`
}
type DiscordInteraction struct {
	ID            string `json:"id"`
	ApplicationID string `json:"application_id"`
	Type          int    `json:"type"`
	Context       *int   `json:"context,omitempty"`
	GuildID       string `json:"guild_id,omitempty"`
	ChannelID     string `json:"channel_id,omitempty"`
	Channel       struct {
		ID   string `json:"id"`
		Type int    `json:"type"`
	} `json:"channel"`
	User struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot,omitempty"`
	} `json:"user"`
	Message struct {
		ID string `json:"id"`
	} `json:"message,omitempty"`
	Data struct {
		Name     string `json:"name,omitempty"`
		CustomID string `json:"custom_id,omitempty"`
		Options  []struct {
			Name  string `json:"name"`
			Type  int    `json:"type"`
			Value string `json:"value"`
		} `json:"options,omitempty"`
		Components []DiscordComponent `json:"components,omitempty"`
	} `json:"data,omitempty"`
}

func discordReplyModal(conv, message string) map[string]any {
	return map[string]any{"type": 9, "data": map[string]any{"custom_id": "bg:reply:" + conv + ":" + message, "title": "Reply to BeepGrid", "components": []any{map[string]any{"type": 18, "label": "Message", "component": map[string]any{"type": 4, "custom_id": "body", "style": 2, "required": true, "max_length": 2000}}}}}
}

// Every canonical mutation and receipt commits before the HTTP acknowledgment.
// Only bot DMs are accepted; guilds and other private-channel contexts are denied.
func (s *Store) ReceiveDiscord(ctx context.Context, c Connection, scope string, in DiscordInteraction) (map[string]any, error) {
	out := map[string]any{}
	if !discordID(in.ID) || in.ApplicationID != c.Config["application_id"] {
		return out, Denied()
	}
	err := s.tx(ctx, Actor{Scope: scope}, func(tx pgx.Tx) error {
		var enabled bool
		var version int64
		if err := tx.QueryRow(ctx, `SELECT enabled,version FROM connections WHERE scope=$1 AND id=$2 AND kind='discord'`, scope, c.ID).Scan(&enabled, &version); err != nil || !enabled {
			return Denied()
		}
		if version != c.Version {
			return Conflict("The installation changed; retry with the current configuration.")
		}
		if in.Type == 1 {
			out["type"] = 1
			return nil
		}
		if in.Context == nil || *in.Context != 1 || in.GuildID != "" || in.Channel.Type != 1 || in.Channel.ID != in.ChannelID || !discordID(in.ChannelID) || !discordID(in.User.ID) || in.User.Bot {
			return Denied()
		}
		p, err := scanPrincipal(tx.QueryRow(ctx, `SELECT `+principalFields+` FROM principals WHERE scope=$1 AND kind='person' AND NOT disabled AND metadata->>'discord_application_id'=$2 AND metadata->>'discord_user_id'=$3`, scope, c.Config["application_id"], in.User.ID))
		if err != nil {
			return Denied()
		}
		a := Actor{Principal: p, Scope: scope, CredentialKind: "provider", ProviderConnection: c.ID}
		a.MessageSource = map[string]string{"provider": "discord", "connection_id": c.ID, "channel_id": in.ChannelID, "thread_id": in.Message.ID}
		key, hash := "discord/"+in.ID, digestJSON(in)
		var previous string
		var raw []byte
		err = tx.QueryRow(ctx, `SELECT input_hash,response FROM provider_events WHERE scope=$1 AND connection_id=$2 AND external_id=$3`, scope, c.ID, key).Scan(&previous, &raw)
		if err == nil {
			if previous != hash {
				return Conflict("Provider interaction ID reused with different content.")
			}
			if err = json.Unmarshal(raw, &out); err != nil {
				return err
			}
			if conv, _ := out["authorization_conversation_id"].(string); conv != "" && !s.canRead(ctx, tx, a, conv) {
				return Denied()
			}
			return nil
		}
		if err != pgx.ErrNoRows {
			return err
		}
		conv, text, source := "", "", in.Message.ID
		parts := strings.Split(in.Data.CustomID, ":")
		if in.Type == 2 {
			if in.Data.Name != "beepgrid" || len(in.Data.Options) != 1 || in.Data.Options[0].Name != "message" || in.Data.Options[0].Type != 3 {
				return Bad("Use /beepgrid message with bounded text.")
			}
			text = in.Data.Options[0].Value
		} else if in.Type == 3 || in.Type == 5 {
			if len(parts) < 3 || parts[0] != "bg" {
				return Bad("Invalid Discord component.")
			}
			if in.Type == 5 {
				if len(parts) != 4 || parts[1] != "reply" || !discordID(parts[3]) {
					return Bad("Invalid Discord reply form.")
				}
				source = parts[3]
			}
			if !discordID(source) {
				return Bad("A mapped Discord source message is required.")
			}
			if err = tx.QueryRow(ctx, `SELECT conversation_id FROM provider_threads WHERE scope=$1 AND connection_id=$2 AND channel_id=$3 AND thread_id=$4 AND person_id=$5`, scope, c.ID, in.ChannelID, source, p.ID).Scan(&conv); err != nil {
				return Denied()
			}
			if parts[1] == "reply" {
				if (in.Type == 3 && len(parts) != 3) || parts[2] != conv {
					return Denied()
				}
				if in.Type == 3 {
					if !s.canRead(ctx, tx, a, conv) {
						return Denied()
					}
					out = discordReplyModal(conv, source)
					out["authorization_conversation_id"] = conv
					return s.recordDiscordInteraction(ctx, tx, scope, c.ID, key, hash, out)
				}
				// Accept exactly the one text input from our modal, in either
				// Discord's label or legacy action-row representation.
				if len(in.Data.Components) != 1 {
					return Bad("A single reply input is required.")
				}
				component := in.Data.Components[0]
				if component.Type == 18 && component.Component != nil {
					component = *component.Component
				} else if component.Type == 1 && len(component.Components) == 1 {
					component = component.Components[0]
				} else {
					return Bad("Invalid reply input.")
				}
				if component.Type != 4 || component.CustomID != "body" {
					return Bad("Invalid reply input.")
				}
				text = component.Value
			} else {
				if in.Type != 3 || len(parts) != 4 || (parts[1] != "a" && parts[1] != "d") {
					return Bad("Invalid approval action.")
				}
				version, err := strconv.Atoi(parts[3])
				if err != nil || version <= 0 {
					return Bad("Invalid request version.")
				}
				q, err := s.request(ctx, tx, a, parts[2])
				if err != nil {
					return err
				}
				if q.ConversationID != conv || q.Kind != "approval" {
					return Denied()
				}
				outcome := "approved"
				if parts[1] == "d" {
					outcome = "denied"
				}
				var result Request
				if err = s.respondTx(ctx, tx, a, q.ID, Response{Version: version, Outcome: outcome}, &result); err != nil {
					return err
				}
			}
		} else {
			return Bad("Unsupported Discord interaction type.")
		}
		if text != "" || in.Type == 2 || in.Type == 5 {
			if !ValidText(text, 2000) {
				return Bad("A reply must contain at most 2000 characters.")
			}
			send := SendInput{ConversationID: conv, Body: text}
			if conv == "" {
				if c.Config["default_agent_id"] == "" {
					return Bad("Configure an authorized default_agent_id for new Discord conversations.")
				}
				send.Target = Target{"agent", c.Config["default_agent_id"]}
				send.Title = "Discord conversation"
			}
			var result SendResult
			if err = s.sendTx(ctx, tx, a, send, "discord-event/"+c.ID+"/"+key, &result); err != nil {
				return err
			}
			conv = result.ConversationID
		}
		out = map[string]any{"type": 4, "data": map[string]any{"content": "Saved in BeepGrid.", "flags": 64, "allowed_mentions": map[string]any{"parse": []string{}}}, "conversation_id": conv, "authorization_conversation_id": conv}
		return s.recordDiscordInteraction(ctx, tx, scope, c.ID, key, hash, out)
	})
	return out, err
}

func (s *Store) recordDiscordInteraction(ctx context.Context, tx pgx.Tx, scope, connection, key, hash string, out map[string]any) error {
	_, err := tx.Exec(ctx, `INSERT INTO provider_events(scope,connection_id,external_id,input_hash,response) VALUES($1,$2,$3,$4,$5)`, scope, connection, key, hash, JSON(out))
	return err
}

func (s *Store) deliverDiscord(ctx context.Context, c Connection, d Delivery, text string) DispatchResult {
	token, err := parseDiscordSecret(c.Secret)
	if err != nil || !discordID(d.Address) || len([]rune(text)) > 2000 {
		return DispatchResult{State: "failed", Detail: "Discord requires valid bot credentials, a linked private user and text within 2000 characters."}
	}
	var channel struct {
		ID         string `json:"id"`
		Type       int    `json:"type"`
		Recipients []struct {
			ID string `json:"id"`
		} `json:"recipients"`
	}
	result := s.discordCall(ctx, token, "/users/@me/channels", map[string]string{"recipient_id": d.Address}, &channel)
	if result.State != "provider_accepted" {
		return result
	}
	if !discordID(channel.ID) || channel.Type != 1 || len(channel.Recipients) != 1 || channel.Recipients[0].ID != d.Address {
		return DispatchResult{State: "failed", Detail: "Discord did not confirm the linked private recipient."}
	}
	conv, _ := d.Payload["conversation_id"].(string)
	buttons := []any{map[string]any{"type": 2, "style": 1, "label": "Reply", "custom_id": "bg:reply:" + conv}}
	if q, ok := d.Payload["request"].(map[string]any); ok && q["kind"] == "approval" && d.Payload["type"] == "request" {
		for _, action := range []struct {
			key, label string
			style      int
		}{{"a", "Approve", 3}, {"d", "Deny", 4}} {
			buttons = append(buttons, map[string]any{"type": 2, "style": action.style, "label": action.label, "custom_id": fmt.Sprintf("bg:%s:%v:%v", action.key, q["id"], q["version"])})
		}
	}
	// Discord's nonce dedup window is limited. Unknown outcomes still require
	// explicit reconciliation rather than an automatic duplicate send.
	body := map[string]any{"content": text, "allowed_mentions": map[string]any{"parse": []string{}}, "nonce": Digest(d.ID)[:24], "enforce_nonce": true, "components": []any{map[string]any{"type": 1, "components": buttons}}}
	var message struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
	}
	result = s.discordCall(ctx, token, "/channels/"+channel.ID+"/messages", body, &message)
	if result.State == "provider_accepted" {
		if !discordID(message.ID) || message.ChannelID != channel.ID {
			return DispatchResult{State: "unknown", Detail: "Discord message receipt was not confirmed."}
		}
		result.Receipt = map[string]string{"channel": channel.ID, "thread": message.ID}
	}
	return result
}

// Fixed provider host, bounded responses, no redirects or credential-bearing errors.
func (s *Store) discordCall(ctx context.Context, token, path string, body, out any) DispatchResult {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://discord.com/api/v10"+path, bytes.NewReader(JSON(body)))
	if err != nil {
		return DispatchResult{State: "failed", Detail: "Invalid Discord request."}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+token)
	transport := s.providerTransport
	if transport == nil {
		t := s.newProviderTransport()
		defer t.CloseIdleConnections()
		transport = t
	}
	client := &http.Client{Timeout: 12 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return DispatchResult{State: "unknown", Detail: "Discord outcome is unknown; reconcile before replay."}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return DispatchResult{State: "unknown", Detail: "Discord response was incomplete."}
	}
	if res.StatusCode == 429 {
		var rate struct {
			RetryAfter float64 `json:"retry_after"`
		}
		_ = json.Unmarshal(raw, &rate)
		delay := time.Duration(min(3600, max(1, rate.RetryAfter)) * float64(time.Second))
		return DispatchResult{State: "failed", Detail: "Discord rate limited the request.", Retry: true, RetryAfter: delay}
	}
	if res.StatusCode >= 500 {
		return DispatchResult{State: "unknown", Detail: "Discord server outcome is unknown; reconcile before replay."}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return DispatchResult{State: "failed", Detail: "Discord rejected the request. Check bot access and private destination."}
	}
	if json.Unmarshal(raw, out) != nil {
		return DispatchResult{State: "unknown", Detail: "Discord response was not readable."}
	}
	return DispatchResult{State: "provider_accepted", Detail: "Discord accepted the message; receipt or reply is not confirmed."}
}
