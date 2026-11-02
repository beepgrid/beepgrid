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
	"sync"
	"time"

	"github.com/beepgrid/beepgrid/api"
	"github.com/beepgrid/beepgrid/internal/engine"
)

type actorKey struct{}
type Server struct {
	limits     rateLimiter
	streamStop chan struct{}
	streamOnce sync.Once
	oidc       *oidcConfig

	Deployment    string
	AccountReader ManagedAccountReader
	Store         *engine.Store
	PublicURL     string
	UI            http.Handler
	mux           *http.ServeMux
}

func New(s *engine.Store, public string, ui http.Handler) *Server {
	x := &Server{streamStop: make(chan struct{}), Store: s, PublicURL: strings.TrimRight(public, "/"), UI: ui, mux: http.NewServeMux()}
	x.routes()
	return x
}

// DrainLiveStreams closes this replica's live transports. Durable event cursors
// let clients resume on another replica; canonical requests are left untouched.
func (s *Server) DrainLiveStreams() { s.streamOnce.Do(func() { close(s.streamStop) }) }

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
	if (r.URL.Path == "/api/v1/auth/login" || strings.HasPrefix(r.URL.Path, "/integrations/")) && !s.limits.allow("unauth/"+remoteIP(r), 120) {
		w.Header().Set("Retry-After", "60")
		writeError(w, &engine.Error{Code: "RATE_LIMITED", Message: "Try again shortly.", Status: 429})
		return
	}
	s.mux.ServeHTTP(w, r)
}
func output(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if result, ok := value.(engine.SendResult); ok && len(result.Messages) > 1 {
		var bytes int
		for _, message := range result.Messages {
			bytes += len(message.Body)
		}
		if bytes >= 1<<20 {
			_ = writeSendResult(w, result)
			return
		}
	}
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
			if c, e := r.Cookie("beepgrid_session"); e == nil {
				token = c.Value
			}
		}
		var a engine.Actor
		var e error
		if r.URL.Path == "/api/v1/workspace/account" && r.Method == "GET" {
			a, e = s.Store.AuthenticateAccount(r.Context(), token)
		} else if r.URL.Path == "/api/v1/auth/logout" && r.Method == "POST" {
			a, e = s.Store.AuthenticateLogout(r.Context(), token)
		} else {
			a, e = s.Store.Authenticate(r.Context(), token)
		}
		if e != nil {
			writeError(w, e)
			return
		}
		if a.CredentialKind == "enrollment" && r.URL.Path != "/api/v1/sessions" {
			writeError(w, engine.Denied())
			return
		}
		permission := credentialRequestPermission(r)
		if !a.CredentialPermits(permission) {
			writeError(w, engine.Denied())
			return
		}
		r = r.WithContext(engine.WithCredentialPermission(r.Context(), permission))
		if !s.limits.allow("credential/"+a.CredentialID, 600) {
			w.Header().Set("Retry-After", "60")
			writeError(w, &engine.Error{Code: "RATE_LIMITED", Message: "Credential request rate exceeded.", Status: 429})
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
	s.workspaceRoutes()
	m := s.mux
	m.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(api.OpenAPI)
	})
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
		http.SetCookie(w, &http.Cookie{Name: "beepgrid_session", Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.PublicURL, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 43200})
		output(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/v1/auth/logout", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		if e := s.Store.Logout(r.Context(), actor(r)); e != nil {
			return e
		}
		http.SetCookie(w, &http.Cookie{Name: "beepgrid_session", Path: "/", HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteStrictMode})
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
			if kind == "agent" {
				x, e := s.Store.Agents(r.Context(), actor(r))
				return ok(w, x, e)
			}
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
	m.HandleFunc("POST /api/v1/people/{id}/profile", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.CreatePerson
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.UpdatePerson(r.Context(), actor(r), r.PathValue("id"), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/appearance", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.AppearanceInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.SaveAppearance(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/people/{id}/status", s.protect(s.setStatus))
	m.HandleFunc("POST /api/v1/agents/{id}/status", s.protect(s.setStatus))
	m.HandleFunc("POST /api/v1/services/{id}/status", s.protect(s.setStatus))
	s.credentialRoutes(m)
	m.HandleFunc("POST /api/v1/credentials", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			PrincipalID string `json:"principal_id"`
			Kind        string `json:"kind"`
			Prefix      string `json:"prefix"`
			engine.CredentialOptions
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.NewCredential(r.Context(), actor(r), in.PrincipalID, in.Kind, in.Prefix, in.CredentialOptions)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/credentials", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Credentials(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/credentials/{id}/revoke", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			RevokeWorkers bool `json:"revoke_workers"`
		}
		// Existing clients revoke without a body. Cascading is explicitly opt-in.
		if r.ContentLength != 0 {
			if e := decode(r, &in); e != nil {
				return e
			}
		}
		return ok(w, map[string]bool{"revoked": true}, s.Store.Revoke(r.Context(), actor(r), r.PathValue("id"), in.RevokeWorkers))
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
	m.HandleFunc("POST /api/v1/agents/{id}/contacts", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.AgentContactsInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.AgentContacts(r.Context(), actor(r), r.PathValue("id"), in)
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
		wait, _ := strconv.Atoi(r.URL.Query().Get("wait_ms"))
		if wait < 0 || wait > 20000 {
			return engine.Bad("wait_ms must be 0 to 20000")
		}
		deadline := time.Now().Add(time.Duration(wait) * time.Millisecond)
		for {
			ready, release := s.Store.WatchWork(actor(r))
			x, e := s.Store.Inbox(r.Context(), actor(r))
			if e != nil || len(x) > 0 || !time.Now().Before(deadline) {
				release()
				return ok(w, x, e)
			}
			pause := min(5*time.Second, time.Until(deadline))
			timer := time.NewTimer(pause)
			select {
			case <-r.Context().Done():
				timer.Stop()
				release()
				return nil
			case <-ready:
				timer.Stop()
			case <-timer.C:
			}
			release()
		}
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
			SubjectID    string         `json:"subject_id"`
			Descriptor   *engine.Action `json:"descriptor"`
			Action       string         `json:"action"`
			Target       engine.Target  `json:"target"`
			ConnectionID string         `json:"connection_id"`
			ActionName   string         `json:"action_name"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.ExplainSubject(r.Context(), actor(r), in.SubjectID, in.Action, in.Target, in.ConnectionID, in.ActionName, in.Descriptor)
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
	m.HandleFunc("POST /api/v1/conversations", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.StartChatInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.StartChat(r.Context(), actor(r), in, r.Header.Get("Idempotency-Key"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/contact-permissions/preview", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.ContactPreviewInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.PreviewContactPermissions(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/contact-permissions", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.ContactPermissionsInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.SaveContactPermissions(r.Context(), actor(r), in)
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
	m.HandleFunc("POST /api/v1/conversations/{id}/membership", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.ChatMembershipInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.ChangeChatMembership(r.Context(), actor(r), r.PathValue("id"), in, r.Header.Get("Idempotency-Key"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/messages", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.SendInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.Send(r.Context(), actor(r), in, r.Header.Get("Idempotency-Key"))
		if e != nil {
			return e
		}
		for _, preference := range strings.Split(r.Header.Get("Prefer"), ",") {
			if strings.EqualFold(strings.TrimSpace(preference), "return=minimal") {
				w.Header().Set("Preference-Applied", "return=minimal")
				return ok(w, map[string]any{"conversation_id": x.ConversationID, "message_count": len(x.Messages)}, nil)
			}
		}
		return ok(w, x, nil)
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
	m.HandleFunc("POST /api/v1/requests/{id}/replace", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.ReplaceRequestInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.ReplaceRequest(r.Context(), actor(r), r.PathValue("id"), in, r.Header.Get("Idempotency-Key"))
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
	m.HandleFunc("GET /api/v1/activity", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, _, e := s.Store.EventPage(r.Context(), actor(r), 0, true)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/events", s.protect(s.events))
	s.channelRoutes()
	s.oidcRoutes()
	s.assistanceRoutes()
	s.attachmentRoutes()
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
	kind := "agent"
	if strings.Contains(r.URL.Path, "/people/") {
		kind = "person"
	}
	if strings.Contains(r.URL.Path, "/services/") {
		kind = "service"
	}
	return ok(w, map[string]bool{"ok": true}, s.Store.SetResourceDisabled(r.Context(), actor(r), kind, r.PathValue("id"), in.Disabled))
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) error {
	cursor := r.Header.Get("Last-Event-ID")
	if cursor == "" {
		cursor = r.URL.Query().Get("after")
	}
	after, _ := strconv.ParseInt(cursor, 10, 64)
	if after < 0 {
		return engine.Bad("Invalid event cursor.")
	}
	conversation := r.URL.Query().Get("conversation_id")
	if len(conversation) > 256 {
		return engine.Bad("Invalid event conversation.")
	}
	if r.URL.Query().Get("stream") != "1" {
		x, e := s.Store.Events(r.Context(), actor(r), after)
		return ok(w, x, e)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	_, ok := w.(http.Flusher)
	if !ok {
		return errors.New("streaming unavailable")
	}
	controller := http.NewResponseController(w)
	defer controller.SetWriteDeadline(time.Time{})
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	ready, release := s.Store.WatchEvents(actor(r))
	defer func() { release() }()
	for {
		// Authenticate on every read, including hints, so an open socket never
		// substitutes for current credentials or event visibility.
		a, authErr := s.Store.RefreshActor(r.Context(), actor(r))
		if authErr != nil {
			return nil
		}
		var events []engine.Event
		var scanned int64
		var e error
		if r.URL.Query().Get("summary") == "1" {
			events, scanned, e = s.Store.EventConversationPage(r.Context(), a, after, conversation)
		} else {
			events, scanned, e = s.Store.EventPage(r.Context(), a, after, false)
		}
		if e != nil {
			return nil
		}
		// A disconnected or non-reading client must not retain a handler and an
		// event page indefinitely. Each finite write batch gets its own deadline.
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		for _, ev := range events {
			if _, e := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Sequence, engine.JSON(ev)); e != nil {
				return nil
			}
			after = ev.Sequence
		}
		if scanned > after {
			if _, e := fmt.Fprintf(w, "id: %d\nevent: cursor\ndata: {}\n\n", scanned); e != nil {
				return nil
			}
			after = scanned
		}
		if _, e := fmt.Fprint(w, ": keepalive\n\n"); e != nil {
			return nil
		}
		if e := controller.Flush(); e != nil {
			return nil
		}
		select {
		case <-r.Context().Done():
			return nil
		case <-s.streamStop:
			return nil
		case <-tick.C:
			// Durable fallback also bounds revocation and missed-hint recovery.
		case <-ready:
			// One closed channel coalesces a burst without an unbounded per-client
			// queue. Batch the first idle wake too, so a short burst does not
			// make every browser repeatedly refetch a growing history.
			coalesce := time.NewTimer(50 * time.Millisecond)
			select {
			case <-r.Context().Done():
				coalesce.Stop()
				return nil
			case <-s.streamStop:
				coalesce.Stop()
				return nil
			case <-coalesce.C:
			}
			release()
			ready, release = s.Store.WatchEvents(actor(r))
		}
	}
}
func ValidatePublicURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return engine.Bad("BEEPGRID_PUBLIC_URL must be an HTTP(S) origin without a path.")
	}
	return nil
}
