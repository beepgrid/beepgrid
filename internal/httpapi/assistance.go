package httpapi

import (
	"github.com/notif-dev/notif/internal/engine"
	"net/http"
)

func (s *Server) assistanceRoutes() {
	m := s.mux
	m.HandleFunc("GET /api/v1/assistance/settings", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.AssistanceSettings(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/assistance/settings", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.AssistanceUpdate
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.UpdateAssistance(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/fleet/plan", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.OverseerInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.PlanOverseer(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/fleet/plans", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.OverseerPlans(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/fleet/plans/{id}/summary", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct{}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.SummarizeOverseer(r.Context(), actor(r), r.PathValue("id"))
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/fleet/plans/{id}", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.OverseerPlan(r.Context(), actor(r), r.PathValue("id"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/requests/{id}/answer-and-share", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			engine.Response
			Audience []string `json:"audience"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.RespondAndShare(r.Context(), actor(r), r.PathValue("id"), in.Response, in.Audience)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/answers", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Answers(r.Context(), actor(r))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/answers", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in engine.PublishInput
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.PublishAnswer(r.Context(), actor(r), in)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/answers/{id}/invalidate", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		return ok(w, map[string]bool{"invalidated": true}, s.Store.InvalidateAnswer(r.Context(), actor(r), r.PathValue("id")))
	}))
	m.HandleFunc("POST /api/v1/requests/{id}/suggestions", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.SuggestAnswers(r.Context(), actor(r), r.PathValue("id"))
		return ok(w, map[string]any{"proposals": x, "matcher_configured": s.Store.Matcher != nil}, e)
	}))
	m.HandleFunc("POST /api/v1/answer-proposals/{id}", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Accept bool `json:"accept"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.DecideProposal(r.Context(), actor(r), r.PathValue("id"), in.Accept)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/fleet/status", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Target *engine.Target `json:"target"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.FleetStatus(r.Context(), actor(r), in.Target)
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/fleet/command", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			PlanID string `json:"plan_id"`
		}
		if e := decode(r, &in); e != nil {
			return e
		}
		x, e := s.Store.ConfirmOverseer(r.Context(), actor(r), in.PlanID)
		return ok(w, x, e)
	}))
}
