package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const telegramFixtureSecret = `{"bot_token":"12345:fixture_token_not_a_real_credential","webhook_secret":"fixture_webhook_secret"}`

func telegramMessage(id, user, message, reply int64, text string) TelegramUpdate {
	raw := fmt.Sprintf(`{"update_id":%d,"message":{"message_id":%d,"from":{"id":%d},"chat":{"id":%d,"type":"private"},"text":%q`, id, message, user, user, text)
	if reply > 0 {
		raw += fmt.Sprintf(`,"reply_to_message":{"message_id":%d}`, reply)
	}
	raw += "}}"
	var out TelegramUpdate
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}
func telegramCallback(id int64, message int64, data string) TelegramUpdate {
	var out TelegramUpdate
	_ = json.Unmarshal([]byte(fmt.Sprintf(`{"update_id":%d,"callback_query":{"id":"callback-%d","from":{"id":42},"message":{"message_id":%d,"chat":{"id":42,"type":"private"}},"data":%q}}`, id, id, message, data)), &out)
	return out
}
func TestTelegramRoundTripDurableDedupApprovalsAndRevoke(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "telegram-worker")
	p := testPerson(t, s, a, "telegram-person")
	grant(t, s, a, p, []Rule{{Action: "view", TargetKind: "agent", TargetID: w.ID}, {Action: "message", TargetKind: "agent", TargetID: w.ID}, {Action: "decide", TargetKind: "agent", TargetID: w.ID}})
	c, err := s.SaveConnection(ctx, a, Connection{Name: "Telegram", Kind: "telegram", Enabled: true, Config: map[string]string{"bot_id": "12345", "default_agent_id": w.ID}, Secret: telegramFixtureSecret})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LinkTelegramAccount(ctx, a, p.ID, "12345", "42"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: p.ID, Address: "43"}); ErrorStatus(err) != 400 {
		t.Fatal("unlinked destination", err)
	}
	d, err := s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: p.ID, Address: "42"})
	if err != nil {
		t.Fatal(err)
	}
	var posted []map[string]any
	s.providerTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" || r.Method != "POST" {
			t.Error("unexpected provider", r.URL.Host)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		posted = append(posted, body)
		raw := fmt.Sprintf(`{"ok":true,"result":{"message_id":%d,"chat":{"id":42,"type":"private"}}}`, 100+len(posted))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw)), Header: http.Header{}}, nil
	})
	first, err := s.Send(ctx, w, SendInput{Target: Target{"person", p.ID}, Body: "Hello Telegram"}, "tg-first")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DispatchOnce(ctx, "https://example.com"); err != nil {
		t.Fatal(err)
	}
	c, scope, err := s.InboundConnection(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	in := telegramMessage(30, 42, 201, 101, "Human reply")
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			out, err := s.ReceiveTelegram(ctx, c, scope, in)
			if err != nil || out["conversation_id"] != first.ConversationID {
				t.Error(out, err)
			}
		})
	}
	wg.Wait()
	var count int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE body='Human reply'`).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate", count)
	}
	changed := telegramMessage(30, 42, 201, 101, "Changed")
	if _, err = s.ReceiveTelegram(ctx, c, scope, changed); ErrorStatus(err) != 409 {
		t.Fatal("changed duplicate", err)
	}
	// Out-of-order updates are independent IDs, not an unsafe maximum offset.
	if _, err = s.ReceiveTelegram(ctx, c, scope, telegramMessage(20, 42, 202, 101, "Earlier update")); err != nil {
		t.Fatal(err)
	}
	unknown := telegramMessage(40, 43, 210, 0, "Unknown user")
	if _, err = s.ReceiveTelegram(ctx, c, scope, unknown); ErrorStatus(err) != 403 {
		t.Fatal("unknown identity", err)
	}
	group := telegramMessage(41, 42, 211, 0, "Group disclosure")
	group.Message.Chat.Type = "group"
	if _, err = s.ReceiveTelegram(ctx, c, scope, group); ErrorStatus(err) != 403 {
		t.Fatal("group accepted", err)
	}
	q, err := s.Ask(ctx, w, AskInput{Target: Target{"person", p.ID}, Kind: "approval", Body: "Deploy?", Action: &Action{Name: "deploy", ExecutorID: w.ID, Resource: "app", ResourceVersion: "v1", OperationKey: "tg-deploy"}}, "tg-approval")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DispatchOnce(ctx, "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 2 || posted[1]["reply_markup"] == nil {
		t.Fatal(posted)
	}
	wrong := telegramCallback(50, 102, fmt.Sprintf("bg:a:%s:%d", q.ID, q.Version+1))
	if _, err = s.ReceiveTelegram(ctx, c, scope, wrong); ErrorStatus(err) != 409 {
		t.Fatal("stale version", err)
	}
	approval := telegramCallback(51, 102, fmt.Sprintf("bg:a:%s:%d", q.ID, q.Version))
	if _, err = s.ReceiveTelegram(ctx, c, scope, approval); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveTelegram(ctx, c, scope, approval); err != nil {
		t.Fatal("approval replay", err)
	}
	current, err := s.Request(ctx, w, q.ID)
	if err != nil || current.Response == nil || current.Response.Outcome != "approved" || *current.ResolvedBy != p.ID {
		t.Fatal(current, err)
	}
	// Relink invalidates both inbound identity and an already queued destination.
	if _, err = s.Send(ctx, w, SendInput{Target: Target{"person", p.ID}, Body: "Queued before relink"}, "tg-stale"); err != nil {
		t.Fatal(err)
	}
	if err = s.LinkTelegramAccount(ctx, a, p.ID, "12345", "44"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveTelegram(ctx, c, scope, in); ErrorStatus(err) != 403 {
		t.Fatal("old link revived", err)
	}
	if err = s.DispatchOnce(ctx, "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 2 {
		t.Fatal("stale destination delivered", posted)
	}
	if err = s.LinkTelegramAccount(ctx, a, p.ID, "12345", "42"); err != nil {
		t.Fatal(err)
	}
	c.Enabled = false
	c.Secret = ""
	c, err = s.SaveConnection(ctx, a, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveTelegram(ctx, c, scope, in); ErrorStatus(err) != 403 {
		t.Fatal("disabled callback", err)
	}
	if _, _, err = s.InboundConnection(ctx, c.ID); ErrorStatus(err) != 404 {
		t.Fatal("disabled ingress", err)
	}
	_ = d
}
func TestTelegramProviderOutcomesAndSecretRedaction(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	c := Connection{Kind: "telegram", Config: map[string]string{"bot_id": "12345"}, Secret: telegramFixtureSecret}
	if !VerifyTelegramSecret(c, "fixture_webhook_secret") || VerifyTelegramSecret(c, "incorrect") {
		t.Fatal("webhook verification")
	}
	if _, err := parseTelegramSecret(telegramFixtureSecret, "67890"); err == nil {
		t.Fatal("wrong bot token")
	}
	for _, tc := range []struct {
		status      int
		body, state string
		retry       bool
	}{{429, `{"ok":false,"parameters":{"retry_after":13}}`, "failed", true}, {503, `{}`, "unknown", false}, {401, `{"ok":false,"description":"token-secret"}`, "failed", false}, {200, `broken`, "unknown", false}, {200, `{"ok":true,"result":true}`, "provider_accepted", false}} {
		s.providerTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
		})
		auth, _ := parseTelegramSecret(c.Secret, c.Config["bot_id"])
		var result bool
		got := s.telegramCall(ctx, auth, "sendMessage", map[string]string{"text": "hi"}, &result)
		if got.State != tc.state || got.Retry != tc.retry || strings.Contains(got.Detail, "token-secret") {
			t.Fatal(got)
		}
	}
	_, err := s.SaveConnection(ctx, a, Connection{Name: "Bot A", Kind: "telegram", Enabled: true, Config: map[string]string{"bot_id": "12345"}, Secret: telegramFixtureSecret})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveConnection(ctx, a, Connection{Name: "Bot duplicate", Kind: "telegram", Enabled: true, Config: map[string]string{"bot_id": "12345"}, Secret: telegramFixtureSecret}); err == nil {
		t.Fatal("same bot accepted twice")
	}
}

func TestTelegramDiagnosticCorrelatesRoundTripAndResetsOnChange(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	p := testPerson(t, s, a, "telegram-diagnostic-person")
	grant(t, s, a, p, []Rule{{Action: "view", TargetKind: "person", TargetID: a.ID}, {Action: "message", TargetKind: "person", TargetID: a.ID}})
	c, err := s.SaveConnection(ctx, a, Connection{Name: "Diagnostic bot", Kind: "telegram", Enabled: true, Config: map[string]string{"bot_id": "12345"}, Secret: telegramFixtureSecret})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LinkTelegramAccount(ctx, a, p.ID, "12345", "42"); err != nil {
		t.Fatal(err)
	}
	d, err := s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: p.ID, Address: "42"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.TestConnection(ctx, a, c.ID, d.ID, "diagnostic-1"); err != nil {
		t.Fatal(err)
	}
	diag, err := s.DiagnoseConnection(ctx, a, c.ID)
	if err != nil || diag.State != "verifying" || diag.Qualification != "not_verified" {
		t.Fatal(diag, err)
	}
	s.providerTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":101,"chat":{"id":42,"type":"private"}}}`))}, nil
	})
	if err = s.DispatchOnce(ctx, "https://example.com"); err != nil {
		t.Fatal(err)
	}
	diag, err = s.DiagnoseConnection(ctx, a, c.ID)
	if err != nil || diag.State != "verifying" || diag.Qualification != "provider_acceptance_observed" {
		t.Fatal(diag, err)
	}
	c, scope, err := s.InboundConnection(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveTelegram(ctx, c, scope, telegramMessage(100, 42, 201, 101, "Received and replied")); err != nil {
		t.Fatal(err)
	}
	diag, err = s.DiagnoseConnection(ctx, a, c.ID)
	if err != nil || diag.State != "connected" || diag.Qualification != "engine_round_trip_observed" || diag.LastTest == nil || !diag.LastTest.InboundReply {
		t.Fatal(diag, err)
	}
	c.Secret = ""
	c, err = s.SaveConnection(ctx, a, c)
	if err != nil {
		t.Fatal(err)
	}
	diag, err = s.DiagnoseConnection(ctx, a, c.ID)
	if err != nil || diag.State != "configured" || diag.LastTest != nil {
		t.Fatal("stale verification survived config change", diag, err)
	}
	c.Enabled = false
	c, err = s.SaveConnection(ctx, a, c)
	if err != nil {
		t.Fatal(err)
	}
	c.Enabled = true
	if _, err = s.SaveConnection(ctx, a, c); ErrorStatus(err) != 409 {
		t.Fatal("reconnect without rotating secret", err)
	}
	c.Secret = strings.ReplaceAll(telegramFixtureSecret, "fixture_webhook_secret", "rotated_webhook_secret")
	c, err = s.SaveConnection(ctx, a, c)
	if err != nil {
		t.Fatal(err)
	}
	if VerifyTelegramSecret(c, "fixture_webhook_secret") {
		t.Fatal("old callback secret revived")
	}
	current, _, err := s.InboundConnection(ctx, c.ID)
	if err != nil || !VerifyTelegramSecret(current, "rotated_webhook_secret") {
		t.Fatal("rotated callback secret", err)
	}
	if err = s.UnlinkTelegramAccount(ctx, a, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveTelegram(ctx, current, scope, telegramMessage(101, 42, 202, 101, "Unlinked")); ErrorStatus(err) != 403 {
		t.Fatal("unlinked identity", err)
	}
}
