package engine

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const discordFixtureSecret = `{"bot_token":"fixture_discord_bot_not_a_real_token"}`

func discordConfig(t *testing.T) map[string]string {
	t.Helper()
	key, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"application_id": "12345", "public_key": hex.EncodeToString(key)}
}

func discordInteraction(id string, kind int) DiscordInteraction {
	var in DiscordInteraction
	_ = json.Unmarshal([]byte(fmt.Sprintf(`{"id":%q,"application_id":"12345","type":%d,"context":1,"channel_id":"91","channel":{"id":"91","type":1},"user":{"id":"42"}}`, id, kind)), &in)
	return in
}

func discordReply(id, conv, source, body string) DiscordInteraction {
	in := discordInteraction(id, 5)
	in.Data.CustomID = "bg:reply:" + conv + ":" + source
	in.Data.Components = []DiscordComponent{{Type: 18, Component: &DiscordComponent{Type: 4, CustomID: "body", Value: body}}}
	return in
}

func TestDiscordSignatureCoversExactBodyAndBoundedTimestamp(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	timestamp := fmt.Sprint(now.Unix())
	body := []byte(`{"type":1}`)
	sign := func(ts string, b []byte) string {
		return hex.EncodeToString(ed25519.Sign(private, append([]byte(ts), b...)))
	}
	key := hex.EncodeToString(public)
	if !VerifyDiscord(key, timestamp, sign(timestamp, body), body, now) {
		t.Fatal("valid signature rejected")
	}
	for _, test := range []struct {
		key, ts, sig string
		body         []byte
	}{
		{key, timestamp, sign(timestamp, body), []byte(`{"type":2}`)},
		{key, fmt.Sprint(now.Add(-6 * time.Minute).Unix()), sign(fmt.Sprint(now.Add(-6*time.Minute).Unix()), body), body},
		{key, fmt.Sprint(now.Add(2 * time.Minute).Unix()), sign(fmt.Sprint(now.Add(2*time.Minute).Unix()), body), body},
		{"bad", timestamp, sign(timestamp, body), body}, {key, timestamp, "bad", body},
	} {
		if VerifyDiscord(test.key, test.ts, test.sig, test.body, now) {
			t.Fatal("forged/stale signature accepted")
		}
	}
}

func TestDiscordDurableReplyDecisionsIsolationAndRelink(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "discord-worker")
	p := testPerson(t, s, a, "discord-person")
	contact := grant(t, s, a, p, []Rule{{Action: "view", TargetKind: "agent", TargetID: w.ID}, {Action: "message", TargetKind: "agent", TargetID: w.ID}, {Action: "decide", TargetKind: "agent", TargetID: w.ID}})
	config := discordConfig(t)
	config["default_agent_id"] = w.ID
	c, err := s.SaveConnection(ctx, a, Connection{Name: "Discord", Kind: "discord", Enabled: true, Config: config, Secret: discordFixtureSecret})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LinkDiscordAccount(ctx, a, p.ID, "12345", "42"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: p.ID, Address: "43"}); ErrorStatus(err) != 400 {
		t.Fatal("unlinked destination", err)
	}
	if _, err = s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: p.ID, Address: "42"}); err != nil {
		t.Fatal(err)
	}
	var posts []map[string]any
	s.providerTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "discord.com" || r.Method != "POST" || r.Header.Get("Authorization") != "Bot fixture_discord_bot_not_a_real_token" {
			t.Error("invalid provider request")
		}
		raw := `{"id":"91","type":1,"recipients":[{"id":"42"}]}`
		if r.URL.Path == "/api/v10/channels/91/messages" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			posts = append(posts, body)
			if body["enforce_nonce"] != true || len(body["nonce"].(string)) > 25 {
				t.Error("missing bounded duplicate protection")
			}
			if len(body["allowed_mentions"].(map[string]any)["parse"].([]any)) != 0 {
				t.Error("unbounded mentions")
			}
			raw = fmt.Sprintf(`{"id":"%d","channel_id":"91"}`, 100+len(posts))
		} else if r.URL.Path != "/api/v10/users/@me/channels" {
			t.Error("unexpected API path")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}, nil
	})
	first, err := s.Send(ctx, w, SendInput{Target: Target{"person", p.ID}, Body: "Hello Discord"}, "discord-first")
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
	open := discordInteraction("20", 3)
	open.Message.ID = "101"
	open.Data.CustomID = "bg:reply:" + first.ConversationID
	modal, err := s.ReceiveDiscord(ctx, c, scope, open)
	if err != nil || modal["type"] != 9 || modal["conversation_id"] != nil {
		t.Fatal("reply modal", modal, err)
	}
	reply := discordReply("30", first.ConversationID, "101", "Human Discord reply")
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			out, err := s.ReceiveDiscord(ctx, c, scope, reply)
			if err != nil || out["conversation_id"] != first.ConversationID {
				t.Error(out, err)
			}
		})
	}
	wg.Wait()
	var count int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE body='Human Discord reply' AND sender_id=$1`, p.ID).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate/wrong attribution", count)
	}
	if err = s.BindRole(ctx, a, Binding{contact.ID, "principal", p.ID}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveDiscord(ctx, c, scope, discordReply("31", first.ConversationID, "101", "After revocation")); ErrorStatus(err) != 403 && ErrorStatus(err) != 404 {
		t.Fatal("revoked contact accepted", err)
	}
	if _, err = s.ReceiveDiscord(ctx, c, scope, reply); ErrorStatus(err) != 403 {
		t.Fatal("revoked replay exposed", err)
	}
	if err = s.BindRole(ctx, a, Binding{contact.ID, "principal", p.ID}, false); err != nil {
		t.Fatal(err)
	}
	changed := discordReply("30", first.ConversationID, "101", "Changed reply")
	if _, err = s.ReceiveDiscord(ctx, c, scope, changed); ErrorStatus(err) != 409 {
		t.Fatal("changed replay", err)
	}
	for name, mutate := range map[string]func(*DiscordInteraction){
		"guild":                 func(in *DiscordInteraction) { in.GuildID = "100" },
		"other private context": func(in *DiscordInteraction) { *in.Context = 2 },
		"group DM":              func(in *DiscordInteraction) { in.Channel.Type = 3 },
		"wrong user":            func(in *DiscordInteraction) { in.User.ID = "43" },
		"wrong app":             func(in *DiscordInteraction) { in.ApplicationID = "12346" },
		"wrong source":          func(in *DiscordInteraction) { in.Data.CustomID = "bg:reply:" + first.ConversationID + ":999" },
	} {
		in := discordReply("40", first.ConversationID, "101", "Denied")
		mutate(&in)
		if _, err = s.ReceiveDiscord(ctx, c, scope, in); ErrorStatus(err) != 403 {
			t.Fatal(name, err)
		}
	}
	q, err := s.Ask(ctx, w, AskInput{Target: Target{"person", p.ID}, Kind: "approval", Body: "Synthetic action?"}, "discord-approval")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DispatchOnce(ctx, "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 || len(posts[1]["components"].([]any)[0].(map[string]any)["components"].([]any)) != 3 {
		t.Fatal("approval buttons", posts)
	}
	approval := discordInteraction("51", 3)
	approval.Message.ID = "102"
	approval.Data.CustomID = fmt.Sprintf("bg:a:%s:%d", q.ID, q.Version+1)
	if _, err = s.ReceiveDiscord(ctx, c, scope, approval); ErrorStatus(err) != 409 {
		t.Fatal("stale version", err)
	}
	approval.Data.CustomID = fmt.Sprintf("bg:a:%s:%d", q.ID, q.Version)
	decisions := contact.Rules
	contact.Rules = contact.Rules[:2]
	contact, err = s.SaveRole(ctx, a, contact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveDiscord(ctx, c, scope, approval); ErrorStatus(err) != 403 {
		t.Fatal("revoked decision accepted", err)
	}
	contact.Rules = decisions
	if _, err = s.SaveRole(ctx, a, contact); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveDiscord(ctx, c, scope, approval); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveDiscord(ctx, c, scope, approval); err != nil {
		t.Fatal("approval replay", err)
	}
	current, err := s.Request(ctx, w, q.ID)
	if err != nil || current.Response == nil || current.Response.Outcome != "approved" || *current.ResolvedBy != p.ID {
		t.Fatal(current, err)
	}
	if _, err = s.Send(ctx, w, SendInput{Target: Target{"person", p.ID}, Body: "Queued before relink"}, "discord-stale"); err != nil {
		t.Fatal(err)
	}
	if err = s.LinkDiscordAccount(ctx, a, p.ID, "12345", "44"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveDiscord(ctx, c, scope, reply); ErrorStatus(err) != 403 {
		t.Fatal("old link revived", err)
	}
	if err = s.DispatchOnce(ctx, "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 {
		t.Fatal("stale destination delivered")
	}
}

func TestDiscordDiagnosticRequiresSubmittedReplyAndResets(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	p := testPerson(t, s, a, "discord-diagnostic")
	grant(t, s, a, p, []Rule{{Action: "view", TargetKind: "person", TargetID: a.ID}, {Action: "message", TargetKind: "person", TargetID: a.ID}})
	c, err := s.SaveConnection(ctx, a, Connection{Name: "Discord", Kind: "discord", Enabled: true, Config: discordConfig(t), Secret: discordFixtureSecret})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LinkDiscordAccount(ctx, a, p.ID, "12345", "42"); err != nil {
		t.Fatal(err)
	}
	d, err := s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: p.ID, Address: "42"})
	if err != nil {
		t.Fatal(err)
	}
	test, err := s.TestConnection(ctx, a, c.ID, d.ID, "discord-diag")
	if err != nil {
		t.Fatal(err)
	}
	s.providerTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw := `{"id":"91","type":1,"recipients":[{"id":"42"}]}`
		if strings.HasSuffix(r.URL.Path, "/messages") {
			raw = `{"id":"101","channel_id":"91"}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}, nil
	})
	if err = s.DispatchOnce(ctx, "https://example.com"); err != nil {
		t.Fatal(err)
	}
	c, scope, err := s.InboundConnection(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	conv := test.Payload["conversation_id"].(string)
	open := discordInteraction("70", 3)
	open.Message.ID = "101"
	open.Data.CustomID = "bg:reply:" + conv
	if _, err = s.ReceiveDiscord(ctx, c, scope, open); err != nil {
		t.Fatal(err)
	}
	diag, err := s.DiagnoseConnection(ctx, a, c.ID)
	if err != nil || diag.Qualification != "provider_acceptance_observed" || diag.LastTest.InboundReply {
		t.Fatal("modal opening counted as reply", diag, err)
	}
	if _, err = s.ReceiveDiscord(ctx, c, scope, discordReply("71", conv, "101", "Confirmed receipt")); err != nil {
		t.Fatal(err)
	}
	diag, err = s.DiagnoseConnection(ctx, a, c.ID)
	if err != nil || diag.State != "connected" || diag.Qualification != "engine_round_trip_observed" {
		t.Fatal(diag, err)
	}
	c.Secret = ""
	c, err = s.SaveConnection(ctx, a, c)
	if err != nil {
		t.Fatal(err)
	}
	diag, err = s.DiagnoseConnection(ctx, a, c.ID)
	if err != nil || diag.LastTest != nil || diag.Qualification != "not_verified" {
		t.Fatal("stale qualification", diag, err)
	}
	c.Enabled = false
	if _, err = s.SaveConnection(ctx, a, c); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveDiscord(ctx, c, scope, discordReply("72", conv, "101", "Old callback")); ErrorStatus(err) != 403 {
		t.Fatal("disabled accepted", err)
	}
}

func TestDiscordProviderUnknownRateLimitAndPrivateRecipient(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	c := Connection{Secret: discordFixtureSecret}
	d := Delivery{ID: "delivery-1", Address: "42", Payload: map[string]any{"conversation_id": "con_test"}}
	for _, status := range []int{429, 500, 403, 200} {
		s.providerTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"retry_after":2,"id":"91","type":3,"recipients":[{"id":"43"}]}`))}, nil
		})
		result := s.deliverDiscord(ctx, c, d, "Private text")
		if status == 429 && (result.State != "failed" || !result.Retry || result.RetryAfter != 2*time.Second) {
			t.Fatal(result)
		}
		if status == 500 && (result.State != "unknown" || result.Retry) {
			t.Fatal(result)
		}
		if (status == 403 || status == 200) && result.State != "failed" {
			t.Fatal("invalid private recipient accepted", result)
		}
		if strings.Contains(result.Detail, "fixture_discord") {
			t.Fatal("secret leaked")
		}
	}
}
