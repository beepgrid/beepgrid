package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/sdk/go/notif"
)

func TestRestrictedTokenHTTPAndCredentialLifecycle(t *testing.T) {
	store, admin, _ := setup(t)
	ctx := context.Background()
	owner, e := store.Authenticate(ctx, admin.Token)
	if e != nil {
		t.Fatal(e)
	}
	p, e := store.CreatePerson(ctx, owner, engine.CreatePerson{ExternalID: "integration", Name: "Integration"})
	if e != nil {
		t.Fatal(e)
	}
	role, e := store.SaveRole(ctx, owner, engine.Role{Name: "Integration access", Rules: []engine.Rule{{Action: "*", TargetKind: "*", TargetID: "*"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = store.BindRole(ctx, owner, engine.Binding{RoleID: role.ID, SubjectKind: "principal", SubjectID: p.ID}, false); e != nil {
		t.Fatal(e)
	}
	until := time.Now().Add(7 * 24 * time.Hour)
	created, e := admin.CreateCredential(ctx, notif.CredentialInput{PrincipalID: p.ID, Kind: "api", Name: "Read-only reports", Permissions: []string{"read"}, ExpiresAt: &until})
	if e != nil {
		t.Fatal(e)
	}
	reader := notif.New(admin.BaseURL, created.Token)
	for _, path := range []string{"/api/v1/me", "/api/v1/credentials", "/api/v1/events"} {
		if e = reader.Do(ctx, "GET", path, nil, nil, ""); e != nil {
			t.Fatal(path, e)
		}
	}
	if e = reader.Do(ctx, "POST", "/api/v1/query", map[string]any{"collection": "agents"}, nil, ""); e != nil {
		t.Fatal("read queries rejected", e)
	}
	for _, path := range []string{"/api/v1/messages", "/api/v1/requests", "/api/v1/people", "/api/v1/fleet/plan", "/api/v1/credentials", "/api/v1/auth/login"} {
		body := map[string]any{}
		if path == "/api/v1/auth/login" {
			body["token"] = created.Token
		}
		e = reader.Do(ctx, "POST", path, body, nil, "")
		if api, ok := e.(*notif.APIError); !ok || api.Status != 403 {
			t.Fatalf("restricted operation %s was not denied: %v", path, e)
		}
	}
	list, e := admin.Credentials(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var meta notif.Credential
	for _, c := range list {
		if c.ID == created.ID {
			meta = c
		}
	}
	if meta.Name != "Read-only reports" || meta.LastUsedAt == nil || meta.Version != 1 || meta.OwnerName != "Integration" {
		t.Fatal("incorrect metadata")
	}
	replacement, e := admin.RotateCredential(ctx, meta.ID, meta.Version, 300)
	if e != nil || replacement.Credential.ID != meta.ID {
		t.Fatal(e)
	}
	updated := notif.New(admin.BaseURL, replacement.Token)
	if e = updated.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); e != nil {
		t.Fatal(e)
	}
	if e = reader.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); e != nil {
		t.Fatal("overlap did not allow switch", e)
	}
	if e = admin.RevokeCredential(ctx, meta.ID, false); e != nil {
		t.Fatal(e)
	}
	for _, client := range []*notif.Client{reader, updated} {
		if e = client.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); e == nil {
			t.Fatal("revoked secret accepted")
		}
	}
}

func TestCredentialRequestPermissionsFailClosed(t *testing.T) {
	cases := map[string]string{
		"GET /api/v1/attachments/one":       "read",
		"POST /api/v1/query":                "read",
		"POST /api/v1/requests/x/responses": "respond",
		"POST /api/v1/requests/x/redeem":    "execute",
		"POST /api/v1/messages":             "message",
		"POST /api/v1/requests":             "request",
		"POST /api/v1/fleet/plan":           "manage",
		"POST /api/v1/new-endpoint":         "manage",
	}
	for request, want := range cases {
		method, path := request[:4], request[5:]
		if request[:3] == "GET" {
			method, path = "GET", request[4:]
		}
		r := httptest.NewRequest(method, path, nil)
		if got := credentialRequestPermission(r); got != want {
			t.Fatalf("%s: %s", request, got)
		}
	}
}
