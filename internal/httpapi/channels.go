package httpapi

import (
	"encoding/json"
	"github.com/notif-dev/notif/internal/engine"
	"io"
	"net/http"
	"net/url"
	"time"
)

func (s *Server) channelRoutes() {
	m := s.mux
	m.HandleFunc("GET /api/v1/connections", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Connections(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/connections", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.Connection
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.SaveConnection(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/destinations", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Destinations(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/destinations", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.Destination
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.SaveDestination(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/people/{id}/slack-account", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			UserID string `json:"user_id"`
			TeamID string `json:"team_id"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		return ok(w, map[string]bool{"linked": true}, s.Store.LinkSlackAccount(r.Context(), actor(r), r.PathValue("id"), in.UserID, in.TeamID))
	}))
	m.HandleFunc("POST /integrations/slack/{id}", func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(io.LimitReader(r.Body, 131073))
		if e != nil || len(body) > 131072 {
			writeError(w, engine.Bad("Invalid callback body."))
			return
		}
		c, scope, e := s.Store.InboundConnection(r.Context(), r.PathValue("id"))
		if e != nil || c.Kind != "slack" {
			writeError(w, engine.Missing())
			return
		}
		var secrets struct {
			BotToken      string `json:"bot_token"`
			SigningSecret string `json:"signing_secret"`
		}
		if json.Unmarshal([]byte(c.Secret), &secrets) != nil || secrets.SigningSecret == "" || !engine.VerifySlack(secrets.SigningSecret, r.Header.Get("X-Slack-Request-Timestamp"), r.Header.Get("X-Slack-Signature"), body, time.Now()) {
			writeError(w, engine.Denied())
			return
		}
		form, e := url.ParseQuery(string(body))
		if e != nil {
			writeError(w, engine.Bad("Invalid Slack payload."))
			return
		}
		var payload struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			Team struct {
				ID string `json:"id"`
			} `json:"team"`
			Actions []struct {
				ActionID string `json:"action_id"`
				Value    string `json:"value"`
			} `json:"actions"`
		}
		if json.Unmarshal([]byte(form.Get("payload")), &payload) != nil || len(payload.Actions) != 1 || payload.Team.ID != c.Config["team_id"] {
			writeError(w, engine.Denied())
			return
		}
		eventID := engine.Digest(string(body))
		if s.Store.HasProviderEvent(r.Context(), scope, c.ID, eventID) {
			output(w, 200, map[string]bool{"ok": true})
			return
		}
		a, e := s.Store.SlackActor(r.Context(), scope, payload.User.ID, payload.Team.ID)
		if e != nil {
			writeError(w, e)
			return
		}
		a.ProviderConnection = c.ID
		var value struct {
			RequestID string `json:"request_id"`
			Version   int    `json:"version"`
		}
		if json.Unmarshal([]byte(payload.Actions[0].Value), &value) != nil {
			writeError(w, engine.Bad("Invalid response action."))
			return
		}
		outcome := ""
		switch payload.Actions[0].ActionID {
		case "notif_approve":
			outcome = "approved"
		case "notif_deny":
			outcome = "denied"
		default:
			writeError(w, engine.Bad("Unsupported Slack action."))
			return
		}
		if _, e = s.Store.Respond(r.Context(), a, value.RequestID, engine.Response{Outcome: outcome, Version: value.Version}); e != nil {
			writeError(w, e)
			return
		}
		if e = s.Store.RecordProviderEvent(r.Context(), scope, c.ID, eventID); e != nil {
			writeError(w, e)
			return
		}
		output(w, 200, map[string]bool{"ok": true})
	})
}
