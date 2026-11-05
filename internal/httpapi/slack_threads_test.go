package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/beepgrid/beepgrid/internal/engine"
)

func TestSlackChannelThreadRepliesAreIsolatedWithoutMentions(t *testing.T) {
	s, client, server := setup(t)
	ctx := context.Background()
	owner, err := s.Authenticate(ctx, client.Token)
	if err != nil {
		t.Fatal(err)
	}
	secret := "fixture-channel-signing-secret"
	c, err := s.SaveConnection(ctx, owner, engine.Connection{Name: "Slack", Kind: "slack", Enabled: true, Config: map[string]string{"team_id": "T1", "default_agent_id": owner.ID}, Secret: string(engine.JSON(map[string]string{"bot_token": "fixture-token-not-real", "signing_secret": secret}))})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LinkSlackAccount(ctx, owner, owner.ID, "U1", "T1"); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreatePerson(ctx, owner, engine.CreatePerson{ExternalID: "other", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	role, err := s.SaveRole(ctx, owner, engine.Role{Name: "Provider replies", Rules: []engine.Rule{{Action: "view", TargetKind: "person", TargetID: p.ID}, {Action: "message", TargetKind: "person", TargetID: p.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindRole(ctx, owner, engine.Binding{RoleID: role.ID, SubjectKind: "principal", SubjectID: owner.ID}, false); err != nil {
		t.Fatal(err)
	}
	first, err := s.Send(ctx, owner, engine.SendInput{Target: engine.Target{Kind: "person", ID: p.ID}, Body: "First conversation"}, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Send(ctx, owner, engine.SendInput{Target: engine.Target{Kind: "person", ID: p.ID}, Body: "Second conversation"}, "second")
	if err != nil {
		t.Fatal(err)
	}
	for i, conv := range []string{first.ConversationID, second.ConversationID} {
		if _, err = s.Pool.Exec(ctx, `INSERT INTO provider_threads(scope,connection_id,channel_id,thread_id,conversation_id,person_id) VALUES($1,$2,'C1',$3,$4,$5)`, owner.Scope, c.ID, strconv.Itoa(i+1)+".0", conv, owner.ID); err != nil {
			t.Fatal(err)
		}
	}
	post := func(eventType, channelType, thread, ts, body, user string) int {
		t.Helper()
		raw := engine.JSON(map[string]any{"type": "event_callback", "team_id": "T1", "event_id": "Ev" + ts + eventType, "event": map[string]string{"type": eventType, "channel_type": channelType, "channel": "C1", "thread_ts": thread, "ts": ts, "text": body, "user": user}})
		now := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte("v0:" + now + ":"))
		mac.Write(raw)
		req, _ := http.NewRequest("POST", server.URL+"/integrations/slack/"+c.ID+"/events", bytes.NewReader(raw))
		req.Header.Set("X-Slack-Request-Timestamp", now)
		req.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return res.StatusCode
	}
	if status := post("message", "channel", "1.0", "3.0", "Reply to first", "U1"); status != 200 {
		t.Fatal(status)
	}
	if status := post("message", "group", "2.0", "4.0", "Reply to second", "U1"); status != 200 {
		t.Fatal(status)
	}
	// Duplicate subscription callbacks must not append the same message twice.
	if status := post("app_mention", "channel", "1.0", "3.0", "Reply to first", "U1"); status != 200 {
		t.Fatal(status)
	}
	for _, item := range []struct{ body, conv string }{{"Reply to first", first.ConversationID}, {"Reply to second", second.ConversationID}} {
		var count int
		_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE body=$1 AND conversation_id=$2`, item.body, item.conv).Scan(&count)
		if count != 1 {
			t.Fatal("misrouted or duplicated thread reply", item, count)
		}
	}
	for _, item := range []struct{ thread, body string }{{"", "Ordinary chatter"}, {"99.0", "Unrelated thread"}} {
		if status := post("message", "channel", item.thread, "5.0", item.body, "U1"); status != 200 {
			t.Fatal(status)
		}
	}
	if status := post("message", "channel", "1.0", "6.0", "Unlinked user", "U2"); status != 403 {
		t.Fatal("unlinked user accepted", status)
	}
	var count int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE body IN ('Ordinary chatter','Unrelated thread','Unlinked user')`).Scan(&count)
	if count != 0 {
		t.Fatal("unrelated channel content ingested")
	}
	// The configured channel API is available headless and remains manager-only.
	var configured json.RawMessage
	if err = client.Do(ctx, "GET", "/api/v1/connections/"+c.ID+"/channels", nil, &configured, ""); err != nil {
		t.Fatal(err)
	}
}
