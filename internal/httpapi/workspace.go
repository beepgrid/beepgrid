package httpapi

import (
	"github.com/notif-dev/notif/internal/engine"
	"net/http"
)

func (s *Server) workspaceRoutes() {
	m := s.mux
	m.HandleFunc("POST /api/v1/roles/preview", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.RolePreviewInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.PreviewRole(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/workspace", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Workspace(r.Context(), actor(r))
		if e != nil {
			return e
		}
		deployment := s.Deployment
		if deployment == "" {
			deployment = "self_hosted"
		}
		return ok(w, map[string]any{"workspace": x, "deployment": deployment, "schema_version": engine.SchemaVersion, "permission_model": engine.PermissionCatalog()}, nil)
	}))
	m.HandleFunc("GET /api/v1/workspace/account", s.protect(s.workspaceAccount))
	m.HandleFunc("GET /api/v1/integrations/catalog", s.protect(func(w http.ResponseWriter, r *http.Request) error { return ok(w, engine.ProviderCatalog(), nil) }))
	m.HandleFunc("GET /api/v1/connections/{id}/diagnostics", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.DiagnoseConnection(r.Context(), actor(r), r.PathValue("id"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/connections/{id}/test", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			DestinationID string `json:"destination_id"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.TestConnection(r.Context(), actor(r), r.PathValue("id"), in.DestinationID, r.Header.Get("Idempotency-Key"))
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/requests/{id}/trace", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.RequestTrace(r.Context(), actor(r), r.PathValue("id"), r.URL.Query().Get("delivery_cursor"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/dispatch-preview", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.DispatchPreviewInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.DispatchPreview(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/message-batches", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.SendBatchInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.SendBatch(r.Context(), actor(r), in, r.Header.Get("Idempotency-Key"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/request-batches", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.AskBatchInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.AskBatch(r.Context(), actor(r), in, r.Header.Get("Idempotency-Key"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/query", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.QueryInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.Query(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/capabilities", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.CapabilityInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.Capabilities(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/access", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Access(r.Context(), actor(r), r.URL.Query().Get("subject_id"))
		return ok(w, x, e)
	}))
}
