package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/notif-dev/notif/internal/engine"
)

type actorKey struct{}
type Server struct {
	Store     *engine.Store
	PublicURL string
	UI        http.Handler
	mux       *http.ServeMux
}

func New(s *engine.Store, public string, ui http.Handler) *Server {
	x := &Server{Store: s, PublicURL: strings.TrimRight(public, "/"), UI: ui, mux: http.NewServeMux()}
	x.routes()
	return x
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'self'; base-uri 'self'; form-action 'self'")
	w.Header().Set("X-Request-ID", engine.ID("trace"))
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	defer func() {
		if v := recover(); v != nil {
			slog.Error("handler panic", "path", r.URL.Path, "type", fmt.Sprintf("%T", v))
			writeError(w, errors.New("internal handler error"))
		}
	}()
	if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
		if o := r.Header.Get("Origin"); o != "" && o != s.PublicURL {
			writeError(w, &engine.Error{Code: "ORIGIN_DENIED", Message: "This origin is not allowed.", Status: 403})
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}
func output(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err error) {
	var e *engine.Error
	if errors.As(err, &e) {
		output(w, e.Status, map[string]any{"error": e})
		return
	}
	slog.Error("request failed", "error", err)
	output(w, 500, map[string]any{"error": map[string]string{"code": "INTERNAL", "message": "The operation failed. Check the server trace."}})
}
func decode(r *http.Request, v any) error {
	if r.Body == nil {
		return engine.Bad("A JSON body is required.")
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 131073))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return engine.Bad("Invalid JSON body: " + e.Error())
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return engine.Bad("Only one JSON value is allowed.")
	}
	return nil
}
func actor(r *http.Request) engine.Actor { return r.Context().Value(actorKey{}).(engine.Actor) }
func (s *Server) protect(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			if c, e := r.Cookie("notif_session"); e == nil {
				token = c.Value
			}
		}
		a, e := s.Store.Authenticate(r.Context(), token)
		if e != nil {
			writeError(w, e)
			return
		}
		if a.CredentialKind == "enrollment" && r.URL.Path != "/api/v1/sessions" {
			writeError(w, engine.Denied())
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), actorKey{}, a))
		if e = fn(w, r); e != nil {
			writeError(w, e)
		}
	}
}
func ok(w http.ResponseWriter, value any, err error) error {
	if err != nil {
		return err
	}
	output(w, 200, value)
	return nil
}
func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { output(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if e := s.Store.Pool.Ping(ctx); e != nil {
			output(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		output(w, 200, map[string]string{"status": "ready"})
	})
	m.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token string `json:"token"`
		}
		if e := decode(r, &in); e != nil {
			writeError(w, e)
			return
		}
		a, e := s.Store.Authenticate(r.Context(), in.Token)
		if e != nil {
			writeError(w, e)
			return
		}
		token, e := s.Store.BrowserSession(r.Context(), a)
		if e != nil {
			writeError(w, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "notif_session", Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.PublicURL, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 43200})
		output(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/v1/auth/logout", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		if e := s.Store.Logout(r.Context(), actor(r)); e != nil {
			return e
		}
		http.SetCookie(w, &http.Cookie{Name: "notif_session", Path: "/", HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteStrictMode})
		output(w, 200, map[string]bool{"ok": true})
		return nil
	}))
	m.HandleFunc("GET /api/v1/me", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Me(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	for _, resource := range []struct{ path, kind string }{{"people", "person"}, {"agents", "agent"}, {"services", "service"}} {
		kind := resource.kind
		m.HandleFunc("GET /api/v1/"+resource.path, s.protect(func(w http.ResponseWriter, r *http.Request) error {
			x, e := s.Store.ListPrincipals(r.Context(), actor(r), kind)
			return ok(w, x, e)
		}))
	}
	m.HandleFunc("POST /api/v1/people", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.CreatePerson
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.CreatePerson(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/people/{id}/status", s.protect(s.setStatus))
	m.HandleFunc("POST /api/v1/agents/{id}/status", s.protect(s.setStatus))
	m.HandleFunc("POST /api/v1/credentials", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			PrincipalID string `json:"principal_id"`
			Kind        string `json:"kind"`
			Prefix      string `json:"prefix"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.NewCredential(r.Context(), actor(r), in.PrincipalID, in.Kind, in.Prefix)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/credentials", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Credentials(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/credentials/{id}/revoke", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		return ok(w, map[string]bool{"revoked": true}, s.Store.Revoke(r.Context(), actor(r), r.PathValue("id")))
	}))
	m.HandleFunc("POST /api/v1/sessions", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.EnrollInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.Enroll(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/agents/{id}/instances", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Instances(r.Context(), actor(r), r.PathValue("id"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/worker/heartbeat", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in map[string]any
		if e := decode(r, &in); e != nil {
			return e
		}
		return ok(w, map[string]bool{"ok": true}, s.Store.Heartbeat(r.Context(), actor(r), in))
	}))
	m.HandleFunc("GET /api/v1/worker/inbox", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Inbox(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/worker/inbox/{id}/ack", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Generation int64 `json:"generation"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		return ok(w, map[string]bool{"received": true}, s.Store.Ack(r.Context(), actor(r), r.PathValue("id"), in.Generation))
	}))
	m.HandleFunc("GET /api/v1/groups", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Groups(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/groups", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.Group
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.SaveGroup(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/roles", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Roles(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/roles", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.Role
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.SaveRole(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/role-bindings", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Bindings(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/role-bindings", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			engine.Binding
			Remove bool `json:"remove"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		return ok(w, map[string]bool{"ok": true}, s.Store.BindRole(r.Context(), actor(r), in.Binding, in.Remove))
	}))
	m.HandleFunc("POST /api/v1/permissions/explain", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Action       string        `json:"action"`
			Target       engine.Target `json:"target"`
			ConnectionID string        `json:"connection_id"`
			ActionName   string        `json:"action_name"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.Explain(r.Context(), actor(r), in.Action, in.Target, in.ConnectionID, in.ActionName)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/route-preview", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Target engine.Target `json:"target"`
			Action string        `json:"action"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.Preview(r.Context(), actor(r), in.Target, in.Action)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/conversations", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Conversations(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/conversations/{id}", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Conversation(r.Context(), actor(r), r.PathValue("id"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/messages", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.SendInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.Send(r.Context(), actor(r), in, r.Header.Get("Idempotency-Key"))
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/requests", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Requests(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/requests/{id}", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Request(r.Context(), actor(r), r.PathValue("id"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/requests", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.AskInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.Ask(r.Context(), actor(r), in, r.Header.Get("Idempotency-Key"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/requests/{id}/responses", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.Response
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.Respond(r.Context(), actor(r), r.PathValue("id"), in)
		return ok(w, x, e)
	}))
	for _, operation := range []struct{ path, state string }{{"cancel", "cancelled"}, {"supersede", "superseded"}} {
		state := operation.state
		m.HandleFunc("POST /api/v1/requests/{id}/"+operation.path, s.protect(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				Version int `json:"version"`
			}
			if e := decode(r, &in); e != nil {
				return e
			}
			x, e := s.Store.EndRequest(r.Context(), actor(r), r.PathValue("id"), state, in.Version)
			return ok(w, x, e)
		}))
	}
	m.HandleFunc("POST /api/v1/requests/{id}/redeem", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.Action
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.Redeem(r.Context(), actor(r), r.PathValue("id"), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/executions", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Executions(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/executions/{id}", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			State  string `json:"state"`
			Result any    `json:"result"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.ReportExecution(r.Context(), actor(r), r.PathValue("id"), in.State, in.Result)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/deliveries", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Deliveries(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/deliveries/{id}/retry", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		return ok(w, map[string]bool{"queued": true}, s.Store.RetryDelivery(r.Context(), actor(r), r.PathValue("id")))
	}))
	m.HandleFunc("GET /api/v1/events", s.protect(s.events))
	s.channelRoutes()
	if s.UI != nil {
		m.Handle("/", s.UI)
	}
}
func (s *Server) setStatus(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Disabled bool `json:"disabled"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	return ok(w, map[string]bool{"ok": true}, s.Store.SetDisabled(r.Context(), actor(r), r.PathValue("id"), in.Disabled))
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) error {
	cursor := r.URL.Query().Get("after")
	if cursor == "" {
		cursor = r.Header.Get("Last-Event-ID")
	}
	after, _ := strconv.ParseInt(cursor, 10, 64)
	if after < 0 {
		return engine.Bad("Invalid event cursor.")
	}
	if r.URL.Query().Get("stream") != "1" {
		x, e := s.Store.Events(r.Context(), actor(r), after)
		return ok(w, x, e)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	f, ok := w.(http.Flusher)
	if !ok {
		return errors.New("streaming unavailable")
	}
	timer := time.NewTimer(25 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		// Authenticate again on every poll so revoked sessions do not keep reading an open socket.
		a, authErr := s.Store.RefreshActor(r.Context(), actor(r))
		if authErr != nil {
			return nil
		}
		var revoked bool
		if e := s.Store.Pool.QueryRow(r.Context(), `SELECT c.revoked OR p.disabled OR (c.expires_at IS NOT NULL AND c.expires_at<=now()) FROM credentials c JOIN principals p ON p.id=c.principal_id WHERE c.scope=$1 AND c.id=$2`, a.Scope, a.CredentialID).Scan(&revoked); e != nil || revoked {
			return nil
		}
		events, e := s.Store.Events(r.Context(), a, after)
		if e != nil {
			return nil
		}
		for _, ev := range events {
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Sequence, engine.JSON(ev))
			after = ev.Sequence
		}
		fmt.Fprint(w, ": keepalive\n\n")
		f.Flush()
		select {
		case <-r.Context().Done():
			return nil
		case <-timer.C:
			return nil
		case <-tick.C:
		}
	}
}
func ValidatePublicURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" {
		return engine.Bad("NOTIF_PUBLIC_URL must be an HTTP(S) origin without a path.")
	}
	return nil
}
