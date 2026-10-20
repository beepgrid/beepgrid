package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/notif-dev/notif/internal/engine"
	"golang.org/x/oauth2"
)

type oidcConfig struct {
	Provider *oidc.Provider
	OAuth    oauth2.Config
	Scope    string
	Issuer   string
	Verifier *oidc.IDTokenVerifier
}
type oidcState struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Issued   int64  `json:"issued"`
}

func (s *Server) ConfigureOIDC(ctx context.Context, scope, issuer, clientID, secret string) error {
	if issuer == "" {
		return nil
	}
	if !strings.HasPrefix(issuer, "https://") && !s.Store.AllowLocal {
		return engine.Bad("OIDC issuer must use HTTPS.")
	}
	provider, e := oidc.NewProvider(ctx, issuer)
	if e != nil {
		return e
	}
	s.oidc = &oidcConfig{provider, oauth2.Config{ClientID: clientID, ClientSecret: secret, RedirectURL: s.PublicURL + "/auth/oidc/callback", Endpoint: provider.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}, scope, issuer, provider.Verifier(&oidc.Config{ClientID: clientID})}
	return nil
}
func (s *Server) encryptState(value oidcState) (string, error) {
	block, e := aes.NewCipher(s.Store.SecretKey)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(g.Seal(nonce, nonce, engine.JSON(value), []byte("notif/oidc/state"))), nil
}
func (s *Server) decryptState(raw string) (oidcState, error) {
	var out oidcState
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil {
		return out, engine.Denied()
	}
	block, e := aes.NewCipher(s.Store.SecretKey)
	if e != nil {
		return out, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil || len(b) < g.NonceSize() {
		return out, engine.Denied()
	}
	plain, e := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], []byte("notif/oidc/state"))
	if e != nil {
		return out, engine.Denied()
	}
	if json.Unmarshal(plain, &out) != nil || time.Now().Unix()-out.Issued > 600 || out.Issued > time.Now().Unix()+30 {
		return out, engine.Denied()
	}
	return out, nil
}
func (s *Server) oidcRoutes() {
	m := s.mux
	m.HandleFunc("GET /api/v1/auth/methods", func(w http.ResponseWriter, r *http.Request) {
		output(w, 200, map[string]any{"token": true, "oidc": s.oidc != nil})
	})
	m.HandleFunc("GET /auth/oidc/start", func(w http.ResponseWriter, r *http.Request) {
		if s.oidc == nil {
			writeError(w, engine.Missing())
			return
		}
		state := oidcState{engine.Secret(), engine.Secret(), oauth2.GenerateVerifier(), time.Now().Unix()}
		encoded, e := s.encryptState(state)
		if e != nil {
			writeError(w, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "notif_oidc", Value: encoded, Path: "/auth/oidc", HttpOnly: true, Secure: strings.HasPrefix(s.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 600})
		http.Redirect(w, r, s.oidc.OAuth.AuthCodeURL(state.State, oauth2.S256ChallengeOption(state.Verifier), oidc.Nonce(state.Nonce)), http.StatusFound)
	})
	m.HandleFunc("GET /auth/oidc/callback", func(w http.ResponseWriter, r *http.Request) {
		if s.oidc == nil {
			writeError(w, engine.Missing())
			return
		}
		cookie, e := r.Cookie("notif_oidc")
		if e != nil {
			writeError(w, engine.Denied())
			return
		}
		state, e := s.decryptState(cookie.Value)
		http.SetCookie(w, &http.Cookie{Name: "notif_oidc", Path: "/auth/oidc", HttpOnly: true, MaxAge: -1})
		if e != nil || r.URL.Query().Get("state") != state.State || r.URL.Query().Get("code") == "" {
			writeError(w, engine.Denied())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		token, e := s.oidc.OAuth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(state.Verifier))
		if e != nil {
			writeError(w, engine.Denied())
			return
		}
		raw, ok := token.Extra("id_token").(string)
		if !ok {
			writeError(w, engine.Denied())
			return
		}
		verified, e := s.oidc.Verifier.Verify(ctx, raw)
		if e != nil || verified.Nonce != state.Nonce {
			writeError(w, engine.Denied())
			return
		}
		var claims struct {
			Name          string `json:"name"`
			Email         string `json:"email"`
			EmailVerified bool   `json:"email_verified"`
		}
		if verified.Claims(&claims) != nil {
			writeError(w, engine.Denied())
			return
		}
		if !claims.EmailVerified {
			claims.Email = ""
		}
		session, e := s.Store.LoginOIDC(ctx, s.oidc.Scope, s.oidc.Issuer, verified.Subject, claims.Name, claims.Email)
		if e != nil {
			writeError(w, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "notif_session", Value: session, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.PublicURL, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 43200})
		http.Redirect(w, r, "/inbox", http.StatusSeeOther)
	})
	m.HandleFunc("POST /api/v1/people/{id}/oidc-account", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Issuer  string `json:"issuer"`
			Subject string `json:"subject"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		return ok(w, map[string]bool{"linked": true}, s.Store.LinkOIDC(r.Context(), actor(r), r.PathValue("id"), in.Issuer, in.Subject))
	}))
	m.HandleFunc("POST /api/v1/services", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Name       string `json:"name"`
			ExternalID string `json:"external_id"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.CreateService(r.Context(), actor(r), in.ExternalID, in.Name)
		return ok(w, x, e)
	}))
}
