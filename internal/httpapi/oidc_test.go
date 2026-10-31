package httpapi

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/beepgrid/beepgrid/internal/engine"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOIDCStateIsEncryptedAuthenticatedAndExpires(t *testing.T) {
	s, _, _ := setup(t)
	h := New(s, "http://localhost", nil)
	raw, e := h.encryptState(oidcState{State: "expected", Nonce: "nonce", Verifier: "private-pkce", Issued: time.Now().Unix()})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(raw, "private-pkce") {
		t.Fatal("state leaks verifier")
	}
	parsed, e := h.decryptState(raw)
	if e != nil || parsed.State != "expected" {
		t.Fatal(parsed, e)
	}
	bytes, _ := base64.RawURLEncoding.DecodeString(raw)
	bytes[len(bytes)-1] ^= 1
	if _, e = h.decryptState(base64.RawURLEncoding.EncodeToString(bytes)); engine.ErrorStatus(e) != 403 {
		t.Fatal("tampered state accepted", e)
	}
	raw, e = h.encryptState(oidcState{State: "expired", Issued: time.Now().Add(-11 * time.Minute).Unix()})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.decryptState(raw); engine.ErrorStatus(e) != 403 {
		t.Fatal("old state accepted", e)
	}
}
func TestOIDCVerifiedCodeFlow(t *testing.T) {
	store, _, _ := setup(t)
	store.AllowLocal = true
	private, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	enc := func(v any) string { return base64.RawURLEncoding.EncodeToString(engine.JSON(v)) }
	var issuer, nonce, challenge string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(private.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(private.E)).Bytes())}}})
		case "/token":
			r.ParseForm()
			hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(hash[:]) != challenge {
				t.Error("PKCE verifier not checked")
			}
			head := enc(map[string]string{"alg": "RS256", "kid": "test"})
			claims := enc(map[string]any{"iss": issuer, "aud": "notif-test", "sub": "employee-1", "nonce": nonce, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "name": "Taylor", "email": "taylor@example.test", "email_verified": true})
			input := head + "." + claims
			digest := sha256.Sum256([]byte(input))
			sig, _ := rsa.SignPKCS1v15(rand.Reader, private, crypto.SHA256, digest[:])
			json.NewEncoder(w).Encode(map[string]any{"access_token": "irrelevant-test-access-token", "token_type": "Bearer", "id_token": input + "." + base64.RawURLEncoding.EncodeToString(sig)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	issuer = provider.URL
	h := New(store, "http://localhost", nil)
	server := httptest.NewServer(h)
	defer server.Close()
	h.PublicURL = server.URL
	if e = h.ConfigureOIDC(context.Background(), "test/dev", issuer, "notif-test", "client-secret"); e != nil {
		t.Fatal(e)
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Get(server.URL + "/auth/oidc/start")
	if e != nil {
		t.Fatal(e)
	}
	location, _ := url.Parse(res.Header.Get("Location"))
	res.Body.Close()
	nonce = location.Query().Get("nonce")
	challenge = location.Query().Get("code_challenge")
	state := location.Query().Get("state")
	if nonce == "" || challenge == "" {
		t.Fatal("OIDC nonce or PKCE absent")
	}
	res, e = client.Get(server.URL + "/auth/oidc/callback?code=test&state=" + url.QueryEscape(state))
	if e != nil {
		t.Fatal(e)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatal(res.Status, string(body))
	}
	res, e = client.Get(server.URL + "/api/v1/me")
	if e != nil {
		t.Fatal(e)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), "Taylor") || strings.Contains(string(body), `"admin":true`) {
		t.Fatal(res.Status, string(body))
	}
}
