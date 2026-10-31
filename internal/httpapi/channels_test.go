package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/beepgrid/beepgrid/internal/engine"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestIncomingWebhookHasFixedIdentityAndDeduplicates(t *testing.T) {
	s, client, server := setup(t)
	ctx := context.Background()
	s.AllowLocal = true
	a, e := s.Authenticate(ctx, client.Token)
	if e != nil {
		t.Fatal(e)
	}
	service, e := s.CreateService(ctx, a, "billing", "Billing service")
	if e != nil {
		t.Fatal(e)
	}
	role, e := s.SaveRole(ctx, a, engine.Role{Name: "Billing contact", Rules: []engine.Rule{{Action: "message", TargetKind: "person", TargetID: a.ID}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, a, engine.Binding{RoleID: role.ID, SubjectKind: "principal", SubjectID: service.ID}, false); e != nil {
		t.Fatal(e)
	}
	secret := "incoming-webhook-signing-secret"
	c, e := s.SaveConnection(ctx, a, engine.Connection{Name: "Billing", Kind: "webhook", Enabled: true, Secret: secret, Config: map[string]string{"url": server.URL, "inbound_sender_id": service.ID}})
	if e != nil {
		t.Fatal(e)
	}
	body := engine.JSON(engine.SendInput{Target: engine.Target{Kind: "person", ID: a.ID}, Body: "Invoice failed"})
	send := func(raw []byte, sign bool) int {
		req, _ := http.NewRequest("POST", server.URL+"/integrations/webhook/"+c.ID, bytes.NewReader(raw))
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req.Header.Set("X-Notif-Timestamp", ts)
		req.Header.Set("X-Notif-Event-ID", "invoice-1")
		if sign {
			req.Header.Set("X-Notif-Signature", engine.Signature(secret, ts, raw))
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return res.StatusCode
	}
	if status := send(body, false); status != 403 {
		t.Fatal(status)
	}
	if status := send(body, true); status != 200 {
		t.Fatal(status)
	}
	if status := send(body, true); status != 200 {
		t.Fatal("duplicate", status)
	}
	var count int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE sender_id=$1`, service.ID).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate messages", count)
	}
	changed := engine.JSON(engine.SendInput{Target: engine.Target{Kind: "person", ID: a.ID}, Body: "Different"})
	if status := send(changed, true); status != 409 {
		t.Fatal("reused ID accepted changed event", status)
	}
	if e = s.SetDisabled(ctx, a, service.ID, true); e != nil {
		t.Fatal(e)
	}
	if status := send(body, true); status != 403 {
		t.Fatal("disabled sender", status)
	}
}
func TestSlackCallbacksVerifyPersonPolicyAndReplay(t *testing.T) {
	s, client, server := setup(t)
	ctx := context.Background()
	a, e := s.Authenticate(ctx, client.Token)
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.CreatePerson(ctx, a, engine.CreatePerson{ExternalID: "manager", Name: "Manager"})
	if e != nil {
		t.Fatal(e)
	}
	secret := "slack-signing-secret-for-tests"
	c, e := s.SaveConnection(ctx, a, engine.Connection{Name: "Slack", Kind: "slack", Config: map[string]string{"team_id": "T1"}, Secret: string(engine.JSON(map[string]string{"bot_token": "xoxb-placeholder-token", "signing_secret": secret})), Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	role, e := s.SaveRole(ctx, a, engine.Role{Name: "Slack approver", Rules: []engine.Rule{{Action: "view", TargetKind: "person", TargetID: a.ID}, {Action: "decide", TargetKind: "person", TargetID: a.ID, ConnectionID: c.ID, ActionName: "deploy"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, a, engine.Binding{RoleID: role.ID, SubjectKind: "principal", SubjectID: p.ID}, false); e != nil {
		t.Fatal(e)
	}
	if e = s.LinkSlackAccount(ctx, a, p.ID, "U1", "T1"); e != nil {
		t.Fatal(e)
	}
	q, e := s.Ask(ctx, a, engine.AskInput{Target: engine.Target{Kind: "person", ID: p.ID}, Kind: "approval", Title: "Deploy v1?", Action: &engine.Action{Name: "deploy", ExecutorID: a.ID, Resource: "app", ResourceVersion: "1", OperationKey: "deploy-1"}}, "slack-approval")
	if e != nil {
		t.Fatal(e)
	}
	callback := func(user, signature string) int {
		payload := map[string]any{"user": map[string]string{"id": user}, "team": map[string]string{"id": "T1"}, "actions": []any{map[string]string{"action_id": "notif_approve", "value": string(engine.JSON(map[string]any{"request_id": q.ID, "version": q.Version}))}}}
		raw := []byte(url.Values{"payload": []string{string(engine.JSON(payload))}}.Encode())
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte("v0:" + ts + ":"))
		h.Write(raw)
		sig := "v0=" + hex.EncodeToString(h.Sum(nil))
		if signature != "" {
			sig = signature
		}
		req, _ := http.NewRequest("POST", server.URL+"/integrations/slack/"+c.ID, bytes.NewReader(raw))
		req.Header.Set("X-Slack-Request-Timestamp", ts)
		req.Header.Set("X-Slack-Signature", sig)
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		var value any
		_ = json.NewDecoder(res.Body).Decode(&value)
		res.Body.Close()
		return res.StatusCode
	}
	if x := callback("U1", "invalid"); x != 403 {
		t.Fatal(x)
	}
	if x := callback("U2", ""); x != 403 {
		t.Fatal("unmapped user", x)
	}
	if x := callback("U1", ""); x != 200 {
		t.Fatal("authorized", x)
	}
	if x := callback("U1", ""); x != 200 {
		t.Fatal("replay", x)
	}
	current, e := s.Request(ctx, a, q.ID)
	if e != nil || current.Response == nil || current.Response.Outcome != "approved" || *current.ResolvedBy != p.ID {
		t.Fatal(current, e)
	}
}
