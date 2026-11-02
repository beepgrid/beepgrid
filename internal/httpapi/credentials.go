package httpapi

import (
	"github.com/beepgrid/beepgrid/internal/engine"
	"net/http"
	"strings"
)

// A closed default keeps new write endpoints unavailable to restricted tokens
// until their operation is classified. Engine identity policy still applies.
func credentialRequestPermission(r *http.Request) string {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if r.Method == "GET" || r.Method == "HEAD" {
		return "read"
	}
	switch p {
	case "query", "capabilities", "permissions/explain", "route-preview", "dispatch-preview", "contact-permissions/preview", "roles/preview", "fleet/status", "worker/heartbeat", "credentials/self/renew":
		return "read"
	case "messages", "message-batches", "conversations", "attachments":
		return "message"
	case "requests", "request-batches":
		return "request"
	}
	if strings.HasPrefix(p, "worker/inbox/") && strings.HasSuffix(p, "/ack") {
		return "read"
	}
	if strings.HasPrefix(p, "conversations/") && strings.HasSuffix(p, "/attachments") {
		return "message"
	}
	if strings.HasPrefix(p, "requests/") {
		if strings.HasSuffix(p, "/responses") || strings.HasSuffix(p, "/answer-and-share") {
			return "respond"
		}
		if strings.HasSuffix(p, "/redeem") {
			return "execute"
		}
		return "request"
	}
	if strings.HasPrefix(p, "executions/") {
		return "execute"
	}
	return "manage"
}
func (s *Server) credentialRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/v1/credentials/{id}/edit", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.CredentialEdit
		if e := decode(r, &in); e != nil {
			return e
		}
		value, e := s.Store.EditCredential(r.Context(), actor(r), r.PathValue("id"), in)
		return ok(w, value, e)
	}))
	m.HandleFunc("POST /api/v1/credentials/{id}/rotate", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		in := engine.CredentialRotation{OverlapSeconds: 3600}
		if e := decode(r, &in); e != nil {
			return e
		}
		value, e := s.Store.RotateCredential(r.Context(), actor(r), r.PathValue("id"), in)
		return ok(w, value, e)
	}))
	m.HandleFunc("POST /api/v1/credentials/self/renew", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		value, e := s.Store.RenewCredential(r.Context(), actor(r))
		return ok(w, value, e)
	}))
}
