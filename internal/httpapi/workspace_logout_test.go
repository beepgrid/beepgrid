package httpapi

import (
	"context"
	"github.com/beepgrid/beepgrid/internal/engine"
	"net/http"
	"strings"
	"testing"
)

func TestOrdinaryBrowserCanLogoutDuringSuspension(t *testing.T) {
	store, admin, server := setup(t)
	ctx := context.Background()
	owner, err := store.Authenticate(ctx, admin.Token)
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.CreatePerson(ctx, owner, engine.CreatePerson{ExternalID: "logout-employee", Name: "Employee"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := store.NewCredential(ctx, owner, p.ID, "api", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.Authenticate(ctx, c["token"].(string))
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.BrowserSession(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ApplyWorkspaceAccess(ctx, owner.Scope, 1, true, owner.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Authenticate(ctx, token); err == nil {
		t.Fatal("suspension allowed normal authentication")
	}
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/auth/logout", strings.NewReader("{}"))
	req.AddCookie(&http.Cookie{Name: "notif_session", Value: token})
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.Status)
	}
	if _, err = store.AuthenticateLogout(ctx, token); err == nil {
		t.Fatal("browser credential was not revoked")
	}
	cleared := false
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "notif_session" && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("browser cookie not cleared")
	}
}
