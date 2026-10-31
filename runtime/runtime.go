// Package runtime composes the public engine, optional UI and background workers.
// Managed deployments can use it without importing Go internal packages or
// introducing commercial dependencies into the self-hosted binary.
package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/internal/httpapi"
	"github.com/beepgrid/beepgrid/internal/web"
)

type ManagedAccountReader = httpapi.ManagedAccountReader
type ManagedAccountSubject = httpapi.ManagedAccountSubject
type ManagedAccount = httpapi.ManagedAccount
type ManagedAccess = httpapi.ManagedAccess

type AttachmentObjectStore = engine.AttachmentObjectStore

// ErrorStatus preserves retry classification without exposing engine internals.
func ErrorStatus(err error) int { return engine.ErrorStatus(err) }

type Config struct {
	Deployment                                     string
	AccountReader                                  ManagedAccountReader
	DatabaseURL, Schema, PublicURL, AttachmentsDir string
	SecretKey                                      []byte
	MaxConnections                                 int32
	Attachments                                    AttachmentObjectStore
	AllowLocal                                     bool
	AI                                             AIConfig
	// DisableUI leaves the authenticated HTTP API and workers available. The
	// headless build tag also omits embedded assets from the resulting binary.
	DisableUI bool
}

type Service struct {
	store  *engine.Store
	api    *httpapi.Server
	public string
}

func Migrate(ctx context.Context, databaseURL, schema string) error {
	return engine.Migrate(ctx, databaseURL, schema)
}

func Open(ctx context.Context, c Config) (*Service, error) {
	if len(c.SecretKey) != 32 {
		return nil, fmt.Errorf("a 32-byte encryption key is required")
	}
	if err := httpapi.ValidatePublicURL(c.PublicURL); err != nil {
		return nil, err
	}
	if c.MaxConnections == 0 {
		c.MaxConnections = 12
	}
	s, err := engine.OpenConfigured(ctx, c.DatabaseURL, c.Schema, c.SecretKey, c.MaxConnections)
	if err != nil {
		return nil, err
	}
	s.AllowLocal, s.AttachmentsRoot, s.AttachmentObjects = c.AllowLocal, c.AttachmentsDir, c.Attachments
	if err := s.ConfigureAI(ctx, c.AI); err != nil {
		s.Close()
		return nil, err
	}
	deployment := c.Deployment
	if deployment == "" {
		deployment = "self_hosted"
	}
	if deployment != "self_hosted" && deployment != "managed" {
		s.Close()
		return nil, fmt.Errorf("invalid deployment kind")
	}
	var ui http.Handler
	if !c.DisableUI {
		ui = web.Handler()
	}
	api := httpapi.New(s, c.PublicURL, ui)
	api.Deployment = deployment
	api.AccountReader = c.AccountReader
	return &Service{store: s, api: api, public: c.PublicURL}, nil
}

func (s *Service) DrainLiveStreams() { s.api.DrainLiveStreams() }

func (s *Service) Close()                { s.store.Close() }
func (s *Service) Handler() http.Handler { return s.api }

var workspaceID = regexp.MustCompile(`^[a-z][a-z0-9_]{2,79}$`)

// ProvisionWorkspace is an operator-only primitive. Callers must authenticate
// management requests separately from customer credentials. Bootstrap is durable
// and accepts replay only with the same credential and scope.
func (s *Service) ProvisionWorkspace(ctx context.Context, id, company, ownerToken string) error {
	if !workspaceID.MatchString(id) || !engine.ValidText(company, 120) {
		return fmt.Errorf("invalid workspace identity or company")
	}
	_, err := s.store.Bootstrap(ctx, id+"/production", company, "production", ownerToken)
	if err != nil && s.store.BootstrapCredentialMatches(ctx, id+"/production", company, ownerToken) {
		err = nil
	}
	if err == nil {
		var current string
		err = s.store.Pool.QueryRow(ctx, `SELECT company_name FROM scopes WHERE id=$1`, id+"/production").Scan(&current)
		if err == nil && current != company {
			return fmt.Errorf("workspace already exists with different company")
		}
	}
	return err
}

func (s *Service) SetSuspended(ctx context.Context, id string, suspended bool) error {
	if !workspaceID.MatchString(id) {
		return fmt.Errorf("invalid workspace identity")
	}
	return s.store.ApplyWorkspaceState(ctx, id+"/production", 0, suspended)
}

// ApplyWorkspaceState applies a monotonically versioned management command.
func (s *Service) ApplyWorkspaceState(ctx context.Context, id string, version int64, suspended bool) error {
	if !workspaceID.MatchString(id) || version < 1 {
		return fmt.Errorf("invalid workspace state")
	}
	return s.store.ApplyWorkspaceState(ctx, id+"/production", version, suspended)
}

// ApplyWorkspaceAccess sets a versioned access deadline independently of the
// customer's credentials. A zero deadline preserves unlimited OSS operation.
func (s *Service) ApplyWorkspaceAccess(ctx context.Context, id string, version int64, suspended bool, until time.Time) error {
	if !workspaceID.MatchString(id) || version < 1 {
		return fmt.Errorf("invalid workspace access")
	}
	return s.store.ApplyWorkspaceAccess(ctx, id+"/production", version, suspended, until)
}

// RunWorkers blocks until cancellation and joins its goroutines before returning.
// All tenants share the query pool, listener and durable work selectors.
func (s *Service) RunWorkers(ctx context.Context) {
	var workers sync.WaitGroup
	workers.Add(3)
	go func() { defer workers.Done(); s.store.RunCoordinatorSummaries(ctx) }()
	go func() {
		defer workers.Done()
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for ctx.Err() == nil {
			step, cancel := context.WithTimeout(ctx, 25*time.Second)
			_, err := s.store.ProcessWorkspaceErasure(step)
			if err == nil {
				_, err = s.store.ProcessAttachmentCleanup(step)
			}
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.Error("workspace erasure requires retry", "error_type", fmt.Sprintf("%T", err))
			}
			select {
			case <-ctx.Done():
			case <-tick.C:
			}
		}
	}()
	go func() {
		defer workers.Done()
		tick := time.NewTicker(300 * time.Millisecond)
		defer tick.Stop()
		for ctx.Err() == nil {
			ready, release := s.store.WatchProviders()
			n, err := s.store.DispatchBatch(ctx, s.public, 32)
			if err != nil && ctx.Err() == nil {
				slog.Error("dispatch failed", "error", err)
			}
			if n == 32 && err == nil {
				release()
				continue
			}
			select {
			case <-ctx.Done():
			case <-tick.C:
			case <-ready:
			}
			release()
		}
	}()
	workers.Wait()
}
