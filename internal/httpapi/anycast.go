package httpapi

import (
	"github.com/beepgrid/beepgrid/internal/engine"
	"net/http"
)

func (s *Server) anycastRoutes() {
	s.mux.HandleFunc("POST /api/v1/anycast", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.AnycastInput
		if e := decode(r, &in); e != nil {
			return e
		}
		out, e := s.Store.Anycast(r.Context(), actor(r), in, r.Header.Get("Idempotency-Key"))
		return ok(w, out, e)
	}))
	s.mux.HandleFunc("GET /api/v1/anycast", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		out, e := s.Store.AnycastDispatches(r.Context(), actor(r))
		return ok(w, out, e)
	}))
	s.mux.HandleFunc("GET /api/v1/anycast/{id}", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		out, e := s.Store.AnycastDispatch(r.Context(), actor(r), r.PathValue("id"))
		return ok(w, out, e)
	}))
	s.mux.HandleFunc("POST /api/v1/anycast/{id}/cancel", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		return ok(w, map[string]bool{"cancelled": true}, s.Store.CancelAnycast(r.Context(), actor(r), r.PathValue("id")))
	}))
	s.mux.HandleFunc("POST /api/v1/worker/anycast/{id}/complete", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		return ok(w, map[string]bool{"completed": true}, s.Store.CompleteAnycast(r.Context(), actor(r), r.PathValue("id")))
	}))
}
