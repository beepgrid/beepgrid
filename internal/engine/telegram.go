package engine

import (
	"bytes"
	"context"
	"crypto/subtle"
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

var telegramIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)
var telegramSecretPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,256}$`)

func telegramID(id string) bool { return telegramIDPattern.MatchString(id) }

type TelegramSecret struct {
	BotToken      string `json:"bot_token"`
	WebhookSecret string `json:"webhook_secret"`
}

func parseTelegramSecret(raw, bot string) (TelegramSecret, error) {
	var out TelegramSecret
	if json.Unmarshal([]byte(raw), &out) != nil || !strings.HasPrefix(out.BotToken, bot+":") || len(out.BotToken) < 25 || len(out.BotToken) > 256 || !regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`).MatchString(out.BotToken) || !telegramSecretPattern.MatchString(out.WebhookSecret) {
		return out, Bad("Telegram requires bot_token for this bot_id and a 16-256 character webhook_secret using letters, numbers, underscore or hyphen.")
	}
	return out, nil
}
func VerifyTelegramSecret(c Connection, header string) bool {
	auth, err := parseTelegramSecret(c.Secret, c.Config["bot_id"])
	return err == nil && subtle.ConstantTimeCompare([]byte(auth.WebhookSecret), []byte(header)) == 1
}

// The administrator attests an immutable user ID, never a mutable username.
// Account linking grants no communication or approval rights.
func (s *Store) LinkTelegramAccount(ctx context.Context, a Actor, personID, botID, userID string) error {
	if !telegramID(botID) || !telegramID(userID) {
		return Bad("Immutable positive Telegram bot_id and user_id are required.")
	}
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.mayLinkAccount(ctx, tx, a, personID) {
			return Denied()
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals WHERE scope=$1 AND kind='person' AND metadata->>'telegram_bot_id'=$2 AND metadata->>'telegram_user_id'=$3 AND id<>$4)`, a.Scope, botID, userID, personID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return Conflict("This Telegram account is already linked.")
		}
		tag, err := tx.Exec(ctx, `UPDATE principals SET metadata=metadata||$3::jsonb WHERE scope=$1 AND id=$2 AND kind='person'`, a.Scope, personID, JSON(map[string]string{"telegram_bot_id": botID, "telegram_user_id": userID}))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return Missing()
		}
		return event(ctx, tx, a, "person.account_linked", "", personID, map[string]string{"provider": "telegram", "verified_by": "workspace_admin"})
	})
}
func (s *Store) telegramDestination(ctx context.Context, db DB, scope, connection, person, address string) bool {
	if !telegramID(address) {
		return false
	}
	var valid bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals p JOIN connections c ON c.scope=p.scope WHERE p.scope=$1 AND p.id=$2 AND p.kind='person' AND NOT p.disabled AND c.id=$3 AND c.kind='telegram' AND p.metadata->>'telegram_user_id'=$4 AND p.metadata->>'telegram_bot_id'=c.config->>'bot_id')`, scope, person, connection, address).Scan(&valid)
	return err == nil && valid
}

func (s *Store) UnlinkTelegramAccount(ctx context.Context, a Actor, personID string) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.mayLinkAccount(ctx, tx, a, personID) {
			return Denied()
		}
		tag, err := tx.Exec(ctx, `UPDATE principals SET metadata=metadata-'telegram_bot_id'-'telegram_user_id' WHERE scope=$1 AND id=$2 AND kind='person'`, a.Scope, personID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return Missing()
		}
		return event(ctx, tx, a, "person.account_unlinked", "", personID, map[string]string{"provider": "telegram"})
	})
}

// Best-effort UI acknowledgement happens only after the canonical commit. It
// never determines whether a decision was accepted and cannot cause replay.
func (s *Store) AcknowledgeTelegramCallback(ctx context.Context, c Connection, id string) {
	if !ValidText(id, 256) {
		return
	}
	auth, err := parseTelegramSecret(c.Secret, c.Config["bot_id"])
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var accepted bool
	s.telegramCall(ctx, auth, "answerCallbackQuery", map[string]string{"callback_query_id": id, "text": "Decision saved. Open the workspace for details."}, &accepted)
}

type TelegramMessage struct {
	ID   int64 `json:"message_id"`
	From struct {
		ID    int64 `json:"id"`
		IsBot bool  `json:"is_bot"`
	} `json:"from"`
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	Text    string           `json:"text"`
	ReplyTo *TelegramMessage `json:"reply_to_message,omitempty"`
}
type TelegramUpdate struct {
	ID       *int64           `json:"update_id"`
	Message  *TelegramMessage `json:"message,omitempty"`
	Callback *struct {
		ID   string `json:"id"`
		From struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		} `json:"from"`
		Message *TelegramMessage `json:"message"`
		Data    string           `json:"data"`
	} `json:"callback_query,omitempty"`
}

// TelegramUpdate commits the message/decision, thread and dedup receipt together.
// The workspace mutation lock serializes disable, credential rotation and relink.
func (s *Store) ReceiveTelegram(ctx context.Context, c Connection, scope string, in TelegramUpdate) (map[string]any, error) {
	out := map[string]any{}
	if in.ID == nil || *in.ID < 0 {
		return out, Bad("A Telegram update_id is required.")
	}
	err := s.tx(ctx, Actor{Scope: scope}, func(tx pgx.Tx) error {
		var enabled bool
		var version int64
		var config []byte
		if err := tx.QueryRow(ctx, `SELECT enabled,version,config FROM connections WHERE scope=$1 AND id=$2 AND kind='telegram'`, scope, c.ID).Scan(&enabled, &version, &config); err != nil || !enabled {
			return Denied()
		}
		if version != c.Version {
			return Conflict("The installation changed; retry with the current credential.")
		}
		if err := json.Unmarshal(config, &c.Config); err != nil {
			return err
		}
		var message *TelegramMessage
		var user int64
		var isBot bool
		if in.Message != nil && in.Callback == nil {
			message = in.Message
			user = message.From.ID
			isBot = message.From.IsBot
		} else if in.Callback != nil && in.Message == nil {
			message = in.Callback.Message
			user = in.Callback.From.ID
			isBot = in.Callback.From.IsBot
		} else {
			return Bad("Unsupported Telegram update type.")
		}
		if message == nil || message.ID <= 0 || message.Chat.Type != "private" || user <= 0 || message.Chat.ID != user || isBot {
			return Denied()
		}
		userID := strconv.FormatInt(user, 10)
		channel := strconv.FormatInt(message.Chat.ID, 10)
		p, err := scanPrincipal(tx.QueryRow(ctx, `SELECT `+principalFields+` FROM principals WHERE scope=$1 AND kind='person' AND NOT disabled AND metadata->>'telegram_bot_id'=$2 AND metadata->>'telegram_user_id'=$3`, scope, c.Config["bot_id"], userID))
		if err != nil {
			return Denied()
		}
		a := Actor{Principal: p, Scope: scope, CredentialKind: "provider", ProviderConnection: c.ID}
		key := "telegram/" + strconv.FormatInt(*in.ID, 10)
		hash := digestJSON(in)
		var previous string
		var raw []byte
		err = tx.QueryRow(ctx, `SELECT input_hash,response FROM provider_events WHERE scope=$1 AND connection_id=$2 AND external_id=$3`, scope, c.ID, key).Scan(&previous, &raw)
		if err == nil {
			if previous != hash {
				return Conflict("Provider update ID reused with different content.")
			}
			if err = json.Unmarshal(raw, &out); err != nil {
				return err
			}
			if conv, _ := out["conversation_id"].(string); conv != "" && !s.canRead(ctx, tx, a, conv) {
				return Denied()
			}
			return nil
		}
		if err != pgx.ErrNoRows {
			return err
		}
		if in.Callback != nil {
			parts := strings.Split(in.Callback.Data, ":")
			if len(parts) != 4 || parts[0] != "bg" || (parts[1] != "a" && parts[1] != "d") {
				return Bad("Invalid Telegram approval action.")
			}
			v, err := strconv.Atoi(parts[3])
			if err != nil || v <= 0 {
				return Bad("Invalid request version.")
			}
			var conv string
			if err = tx.QueryRow(ctx, `SELECT conversation_id FROM provider_threads WHERE scope=$1 AND connection_id=$2 AND channel_id=$3 AND thread_id=$4 AND person_id=$5`, scope, c.ID, channel, strconv.FormatInt(message.ID, 10), p.ID).Scan(&conv); err != nil {
				return Denied()
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
			if err = s.respondTx(ctx, tx, a, q.ID, Response{Version: v, Outcome: outcome}, &result); err != nil {
				return err
			}
			out = map[string]any{"conversation_id": conv, "request_id": q.ID, "outcome": outcome}
		} else {
			if !ValidText(message.Text, 32000) {
				return Bad("A bounded text message is required. Files are available in the workspace.")
			}
			conv := ""
			if message.ReplyTo != nil {
				if err = tx.QueryRow(ctx, `SELECT conversation_id FROM provider_threads WHERE scope=$1 AND connection_id=$2 AND channel_id=$3 AND thread_id=$4 AND person_id=$5`, scope, c.ID, channel, strconv.FormatInt(message.ReplyTo.ID, 10), p.ID).Scan(&conv); err != nil {
					return Bad("Reply target is not a mapped Beep Grid message.")
				}
			}
			send := SendInput{ConversationID: conv, Body: message.Text}
			if conv == "" {
				if c.Config["default_agent_id"] == "" {
					return Bad("Configure an authorized default_agent_id for new Telegram conversations.")
				}
				send.Target = Target{"agent", c.Config["default_agent_id"]}
				send.Title = "Telegram conversation"
			}
			var result SendResult
			if err = s.sendTx(ctx, tx, a, send, "telegram-event/"+c.ID+"/"+key, &result); err != nil {
				return err
			}
			if err = s.recordThread(ctx, tx, scope, c.ID, channel, strconv.FormatInt(message.ID, 10), result.ConversationID, p.ID); err != nil {
				return err
			}
			out = map[string]any{"conversation_id": result.ConversationID, "message_count": len(result.Messages)}
		}
		_, err = tx.Exec(ctx, `INSERT INTO provider_events(scope,connection_id,external_id,input_hash,response) VALUES($1,$2,$3,$4,$5)`, scope, c.ID, key, hash, JSON(out))
		return err
	})
	return out, err
}

func (s *Store) deliverTelegram(ctx context.Context, c Connection, d Delivery, text string) DispatchResult {
	auth, err := parseTelegramSecret(c.Secret, c.Config["bot_id"])
	if err != nil {
		return DispatchResult{State: "failed", Detail: "Telegram credentials are invalid."}
	}
	if !telegramID(d.Address) || len([]rune(text)) > 4096 {
		return DispatchResult{State: "failed", Detail: "Telegram requires a private user destination and text within 4096 characters. Continue in the workspace."}
	}
	body := map[string]any{"chat_id": d.Address, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}
	conv, _ := d.Payload["conversation_id"].(string)
	thread := ""
	err = s.Pool.QueryRow(ctx, `SELECT thread_id FROM provider_threads WHERE connection_id=$1 AND conversation_id=$2 AND channel_id=$3 AND person_id=$4 ORDER BY created_at DESC LIMIT 1`, c.ID, conv, d.Address, d.RecipientID).Scan(&thread)
	if err != nil && err != pgx.ErrNoRows {
		return DispatchResult{State: "failed", Detail: "Telegram thread lookup failed.", Retry: true}
	}
	if id, err := strconv.ParseInt(thread, 10, 64); err == nil {
		body["reply_parameters"] = map[string]any{"message_id": id}
	}
	if q, ok := d.Payload["request"].(map[string]any); ok && q["kind"] == "approval" && d.Payload["type"] == "request" {
		approve := fmt.Sprintf("bg:a:%v:%v", q["id"], q["version"])
		deny := fmt.Sprintf("bg:d:%v:%v", q["id"], q["version"])
		if len(approve) > 64 || len(deny) > 64 {
			return DispatchResult{State: "failed", Detail: "Telegram approval data exceeds provider bounds."}
		}
		body["reply_markup"] = map[string]any{"inline_keyboard": [][]map[string]string{{{"text": "Approve", "callback_data": approve}, {"text": "Deny", "callback_data": deny}}}}
	}
	var result struct {
		ID   int64 `json:"message_id"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
	}
	outcome := s.telegramCall(ctx, auth, "sendMessage", body, &result)
	if outcome.State == "provider_accepted" {
		if result.ID <= 0 || strconv.FormatInt(result.Chat.ID, 10) != d.Address || result.Chat.Type != "private" {
			return DispatchResult{State: "unknown", Detail: "Telegram receipt did not confirm the private destination."}
		}
		outcome.Receipt = map[string]string{"channel": d.Address, "thread": strconv.FormatInt(result.ID, 10)}
	}
	return outcome
}

// Errors never include token-bearing URLs or provider-supplied descriptions.
func (s *Store) telegramCall(ctx context.Context, auth TelegramSecret, method string, body any, out any) DispatchResult {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.telegram.org/bot"+auth.BotToken+"/"+method, bytes.NewReader(JSON(body)))
	if err != nil {
		return DispatchResult{State: "failed", Detail: "Invalid Telegram request."}
	}
	req.Header.Set("Content-Type", "application/json")
	transport := s.providerTransport
	if transport == nil {
		t := s.newProviderTransport()
		defer t.CloseIdleConnections()
		transport = t
	}
	client := &http.Client{Timeout: 12 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return DispatchResult{State: "unknown", Detail: "Telegram response was not confirmed; reconcile before replay."}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return DispatchResult{State: "unknown", Detail: "Telegram response was incomplete."}
	}
	var envelope struct {
		OK         bool            `json:"ok"`
		Code       int             `json:"error_code"`
		Result     json.RawMessage `json:"result"`
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	parsed := json.Unmarshal(raw, &envelope) == nil
	if res.StatusCode == 429 || (parsed && !envelope.OK && envelope.Code == 429) {
		return DispatchResult{State: "failed", Detail: "Telegram rate limited the request.", Retry: true, RetryAfter: min(time.Hour, time.Duration(max(1, envelope.Parameters.RetryAfter))*time.Second)}
	}
	if res.StatusCode >= 500 {
		return DispatchResult{State: "unknown", Detail: "Telegram server outcome is unknown; reconcile before replay."}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || (parsed && !envelope.OK) {
		return DispatchResult{State: "failed", Detail: "Telegram rejected the request. Check credentials, bot access and destination."}
	}
	if !parsed || len(envelope.Result) == 0 || json.Unmarshal(envelope.Result, out) != nil {
		return DispatchResult{State: "unknown", Detail: "Telegram response was not readable."}
	}
	return DispatchResult{State: "provider_accepted", Detail: "Telegram accepted the message; receipt or reply is not confirmed."}
}
