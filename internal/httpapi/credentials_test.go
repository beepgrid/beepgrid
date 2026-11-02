package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/sdk/go"
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
	created, e := admin.CreateCredential(ctx, beepgrid.CredentialInput{PrincipalID: p.ID, Kind: "api", Name: "Read-only reports", Permissions: []string{"read"}, ExpiresAt: &until})
	if e != nil {
		t.Fatal(e)
	}
	reader := beepgrid.New(admin.BaseURL, created.Token)
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
		if api, ok := e.(*beepgrid.APIError); !ok || api.Status != 403 {
			t.Fatalf("restricted operation %s was not denied: %v", path, e)
		}
	}
	list, e := admin.Credentials(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var meta beepgrid.Credential
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
	updated := beepgrid.New(admin.BaseURL, replacement.Token)
	if e = updated.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); e != nil {
		t.Fatal(e)
	}
	if e = reader.Do(ctx, "GET", "/api/v1/me", nil, nil, ""); e != nil {
		t.Fatal("overlap did not allow switch", e)
	}
	if e = admin.Do(ctx, "POST", "/api/v1/credentials/"+meta.ID+"/revoke", nil, nil, ""); e != nil {
		t.Fatal("legacy bodyless revocation failed", e)
	}
	if e = admin.RevokeCredential(ctx, meta.ID, false); e != nil {
		t.Fatal(e)
	}
	for _, client := range []*beepgrid.Client{reader, updated} {
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

func TestNeverExpiringGoClientLifecycle(t *testing.T) {
	_, admin, _ := setup(t)
	ctx := context.Background()
	var me struct {
		Person struct {
			ID string `json:"id"`
		} `json:"person"`
	}
	if err := admin.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); err != nil {
		t.Fatal(err)
	}
	issued, err := admin.CreateCredential(ctx, beepgrid.CredentialInput{PrincipalID: me.Person.ID, Kind: "api", NeverExpires: true})
	if err != nil || !issued.ExpiresAt.IsZero() {
		t.Fatal("nullable API expiry did not round trip", err)
	}
	worker, session, err := admin.Enroll(ctx, beepgrid.EnrollInput{Agent: "permanent/go", Name: "Persistent Go", NeverExpires: true})
	if err != nil || !session.ExpiresAt.IsZero() {
		t.Fatal("nullable worker expiry did not round trip", err)
	}
	if err = worker.Heartbeat(ctx, nil); err != nil {
		t.Fatal(err)
	}
}
