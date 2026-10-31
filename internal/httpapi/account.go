package httpapi

import (
	"context"
	"github.com/beepgrid/beepgrid/internal/engine"
	"net/http"
	"time"
)

// A server-side composition seam. No operator/member token or arbitrary URL is
// accepted from the customer. The callback receives authenticated scope only.
type ManagedAccountSubject struct {
	Scope    string `json:"scope"`
	PersonID string `json:"person_id"`
}
type ManagedAccess struct {
	Enabled    bool   `json:"enabled"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
	ValidUntil int64  `json:"valid_until"`
	Revision   int64  `json:"revision"`
}
type ManagedAccount struct {
	QuotaPolicy   *engine.WorkspaceBudget `json:"quota_policy"`
	QuotaRevision int64                   `json:"quota_revision"`
	ID            string                  `json:"id"`
	Company       string                  `json:"company"`
	Region        string                  `json:"region"`
	State         string                  `json:"state"`
	Desired       string                  `json:"desired"`
	Version       int64                   `json:"version"`
	Access        ManagedAccess           `json:"access"`
}
type ManagedAccountReader func(context.Context, ManagedAccountSubject) (ManagedAccount, error)

func (s *Server) workspaceAccount(w http.ResponseWriter, r *http.Request) error {
	a := actor(r)
	snapshot, e := s.Store.Account(r.Context(), a)
	if e != nil {
		return e
	}
	deployment := s.Deployment
	if deployment == "" {
		deployment = "self_hosted"
	}
	result := map[string]any{"deployment": deployment, "applied": snapshot}
	if deployment == "managed" {
		if s.AccountReader == nil {
			return &engine.Error{Code: "ACCOUNT_UNAVAILABLE", Message: "Managed account information is unavailable.", Status: 503}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		managed, e := s.AccountReader(ctx, ManagedAccountSubject{Scope: a.Scope, PersonID: a.ID})
		if e != nil || managed.ID+"/"+snapshot.Environment != a.Scope {
			return &engine.Error{Code: "ACCOUNT_UNAVAILABLE", Message: "Managed account information is unavailable.", Status: 503}
		}
		// Recheck grants after the external read, without holding a workspace lock.
		if _, e = s.Store.Account(r.Context(), a); e != nil {
			return e
		}
		result["managed"] = managed
	}
	return ok(w, result, nil)
}
