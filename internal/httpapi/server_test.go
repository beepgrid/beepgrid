package httpapi

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/notif-dev/notif/internal/engine"
	"github.com/notif-dev/notif/sdk/go/notif"
)

func setup(t *testing.T) (*engine.Store, *notif.Client, *httptest.Server) {
	t.Helper()
	database := os.Getenv("NOTIF_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("Set NOTIF_TEST_DATABASE_URL to run HTTP and SDK integration tests.")
	}
	ctx := context.Background()
	schema := "api_" + strings.TrimPrefix(engine.ID(""), "_")
	s, e := engine.Open(ctx, database, schema, []byte("01234567890123456789012345678901"))
	if e != nil {
		t.Fatal(e)
	}
	token := engine.Secret()
	if _, e = s.Bootstrap(ctx, "test/dev", "Test company", "dev", token); e != nil {
		t.Fatal(e)
	}
	h := New(s, "http://localhost", nil)
	server := httptest.NewServer(h)
	h.PublicURL = server.URL
	t.Cleanup(func() { server.Close(); _, _ = s.Pool.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); s.Close() })
	return s, notif.New(server.URL, token), server
}
func TestGoSDKHTTPJourney(t *testing.T) {
	_, admin, _ := setup(t)
	ctx := context.Background()
	var me struct {
		Person notif.Person `json:"person"`
	}
	if e := admin.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); e != nil {
		t.Fatal(e)
	}
	worker, session, e := admin.Enroll(ctx, notif.EnrollInput{Agent: "test/scout", Name: "Scout", Capabilities: []string{"message"}})
	if e != nil {
		t.Fatal(e)
	}
	var role struct {
		ID string `json:"id"`
	}
	if e = admin.Do(ctx, "POST", "/api/v1/roles", map[string]any{"name": "Worker", "rules": []map[string]string{{"action": "view", "target_kind": "*", "target_id": "*"}, {"action": "message", "target_kind": "person", "target_id": me.Person.ID}, {"action": "ask", "target_kind": "person", "target_id": me.Person.ID}}}, &role, ""); e != nil {
		t.Fatal(e)
	}
	if e = admin.Do(ctx, "POST", "/api/v1/role-bindings", map[string]any{"role_id": role.ID, "subject_kind": "principal", "subject_id": session.Agent.ID}, nil, ""); e != nil {
		t.Fatal(e)
	}
	q, e := worker.Ask(ctx, notif.AskInput{Target: notif.Target{Kind: "person", ID: me.Person.ID}, Title: "Can I continue?", Choices: []string{"Continue", "Wait"}}, "sdk-ask")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Send(ctx, notif.SendInput{ConversationID: q.ConversationID, Body: "What did you finish?"}, "clarification"); e != nil {
		t.Fatal(e)
	}
	jobs, e := worker.Inbox(ctx)
	if e != nil || len(jobs) != 1 {
		t.Fatal(jobs, e)
	}
	if e = worker.Ack(ctx, jobs[0]); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Respond(ctx, q.ID, notif.Response{Outcome: "answered", Answer: "Continue", Version: q.Version}); e != nil {
		t.Fatal(e)
	}
	result, e := worker.Wait(ctx, q.ID)
	if e != nil || result.Response == nil || result.Response.Answer != "Continue" {
		t.Fatal(result, e)
	}
	jobs, e = worker.Inbox(ctx)
	if e != nil || len(jobs) != 1 || jobs[0].Payload.Type != "request.resolved" {
		t.Fatal(jobs, e)
	}
}
func TestBrowserSessionOriginAndRevocation(t *testing.T) {
	store, admin, server := setup(t)
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/auth/login", strings.NewReader(`{"token":"`+admin.Token+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, e := browser.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.Status)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe browser cookie")
	}
	req, _ = http.NewRequest("POST", server.URL+"/api/v1/groups", strings.NewReader(`{"name":"evil","members":[]}`))
	req.Header.Set("Origin", "https://evil.example")
	resp, e = browser.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("cross-origin mutation accepted")
	}
	resp, e = browser.Get(server.URL + "/api/v1/me")
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.Status)
	}
	if _, e = store.Pool.Exec(context.Background(), `UPDATE credentials SET revoked=true WHERE kind='browser'`); e != nil {
		t.Fatal(e)
	}
	resp, e = browser.Get(server.URL + "/api/v1/me")
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("revoked cookie remained authorized")
	}
}

func TestSDKRetriesLostResponseAfterCommitWithoutDuplicate(t *testing.T) {
	store, original, _ := setup(t)
	ctx := context.Background()
	var first atomic.Bool
	handler := New(store, "http://localhost", nil)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/messages" && !first.Swap(true) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			if recorder.Code != 200 {
				t.Error("fixture did not commit", recorder.Code)
			}
			connection, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			connection.Close()
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	handler.PublicURL = proxy.URL
	client := notif.New(proxy.URL, original.Token)
	var me struct {
		Person notif.Person `json:"person"`
	}
	if e := client.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); e != nil {
		t.Fatal(e)
	}
	result, e := client.Send(ctx, notif.SendInput{Target: notif.Target{Kind: "person", ID: me.Person.ID}, Body: "Committed before reply was lost"}, "lost-reply")
	if e != nil || len(result.Messages) != 1 {
		t.Fatal(result, e)
	}
	var count int
	_ = store.Pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE body='Committed before reply was lost'`).Scan(&count)
	if count != 1 {
		t.Fatal("network replay duplicated accepted work", count)
	}
}

func TestGoSDKApprovalExecutionLifecycle(t *testing.T) {
	_, admin, _ := setup(t)
	ctx := context.Background()
	var me struct {
		Person notif.Person `json:"person"`
	}
	if e := admin.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); e != nil {
		t.Fatal(e)
	}
	worker, session, e := admin.Enroll(ctx, notif.EnrollInput{Agent: "release/test", Name: "Executor"})
	if e != nil {
		t.Fatal(e)
	}
	action := notif.Action{Name: "local_test", ExecutorID: session.Agent.ID, Resource: "fixture", ResourceVersion: "1", OperationKey: "fixture/1", Arguments: map[string]any{"text": "ok"}}
	q, e := admin.Ask(ctx, notif.AskInput{Kind: "approval", Title: "Fixture?", Target: notif.Target{Kind: "person", ID: me.Person.ID}, Action: &action}, "approval")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = worker.Redeem(ctx, q.ID, action); e == nil {
		t.Fatal("unapproved action redeemed")
	}
	if _, e = admin.Respond(ctx, q.ID, notif.Response{Outcome: "approved", Version: q.Version}); e != nil {
		t.Fatal(e)
	}
	execution, e := worker.Redeem(ctx, q.ID, action)
	if e != nil {
		t.Fatal(e)
	}
	again, e := worker.Redeem(ctx, q.ID, action)
	if e != nil || again.ID != execution.ID {
		t.Fatal("duplicate intent", again, e)
	}
	altered := action
	altered.ResourceVersion = "2"
	if _, e = worker.Redeem(ctx, q.ID, altered); e == nil {
		t.Fatal("altered action accepted")
	}
	result, e := worker.ReportExecution(ctx, execution.ID, "succeeded", map[string]any{"effect": "test-reported"})
	if e != nil || result.State != "succeeded" {
		t.Fatal(result, e)
	}
	if _, e = worker.ReportExecution(ctx, execution.ID, "failed", nil); e == nil {
		t.Fatal("terminal report overwritten")
	}
}
