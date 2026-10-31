package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/internal/web"
	"github.com/beepgrid/beepgrid/sdk/go/notif"
)

func TestOptionalWorkspaceUsesSameEngineAndAuthorization(t *testing.T) {
	full, config := service(t)
	ctx := context.Background()
	token := engine.Secret()
	if err := full.ProvisionWorkspace(ctx, "optional_ui", "Optional workspace", token); err != nil {
		t.Fatal(err)
	}
	config.DisableUI = true
	headless, err := Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer headless.Close()
	fullHTTP := httptest.NewServer(full.Handler())
	defer fullHTTP.Close()
	headlessHTTP := httptest.NewServer(headless.Handler())
	defer headlessHTTP.Close()
	for _, path := range []string{"/", "/agents", "/fonts/not-real.woff2"} {
		res, err := http.Get(headlessHTTP.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatal("disabled workspace still serves a browser route", path, res.StatusCode)
		}
	}
	res, err := http.Get(fullHTTP.URL + "/agents")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	want := http.StatusNotFound
	if web.Available {
		want = http.StatusOK
	}
	if res.StatusCode != want {
		t.Fatal("default workspace packaging changed", res.StatusCode)
	}
	cli := notif.New(headlessHTTP.URL, token)
	var me struct{ Person notif.Person }
	if err = cli.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); err != nil {
		t.Fatal(err)
	}
	worker, session, err := cli.Enroll(ctx, notif.EnrollInput{Agent: "headless/scout", Name: "Scout"})
	if err != nil {
		t.Fatal(err)
	}
	var role struct{ ID string }
	if err = cli.Do(ctx, "POST", "/api/v1/roles", map[string]any{"name": "Owner contact", "rules": []map[string]string{{"action": "ask", "target_kind": "person", "target_id": me.Person.ID}}}, &role, ""); err != nil {
		t.Fatal(err)
	}
	if err = cli.Do(ctx, "POST", "/api/v1/role-bindings", map[string]string{"role_id": role.ID, "subject_kind": "principal", "subject_id": session.Agent.ID}, nil, ""); err != nil {
		t.Fatal(err)
	}
	q, err := worker.Ask(ctx, notif.AskInput{Target: notif.Target{Kind: "person", ID: me.Person.ID}, Title: "Report status"}, "shared-across-ui")
	if err != nil {
		t.Fatal(err)
	}
	// Owner authorization is identical from either API composition.
	ui := notif.New(fullHTTP.URL, token)
	if got, err := ui.GetRequest(ctx, q.ID); err != nil || got.ID != q.ID {
		t.Fatal("workspace cannot read the headless request", got, err)
	}
	if got, err := ui.Respond(ctx, q.ID, notif.Response{Outcome: "answered", Version: q.Version, Answer: "Ready"}); err != nil || got.State != "resolved" {
		t.Fatal("workspace response failed", got, err)
	}
	if got, err := cli.GetRequest(ctx, q.ID); err != nil || got.Response == nil || got.Response.Answer != "Ready" {
		t.Fatal("canonical result differs across clients", got, err)
	}
	if err = notif.New(headlessHTTP.URL, "invalid").Do(ctx, "GET", "/api/v1/me", nil, nil, ""); err == nil {
		t.Fatal("headless mode bypassed authentication")
	}
	if _, err = worker.Assistance(ctx); err != nil {
		t.Fatal("worker API unavailable without UI", err)
	}
}
