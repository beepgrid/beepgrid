// Package runtime composes the public engine, embedded UI and background workers.
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

	"github.com/notif-dev/notif/internal/engine"
	"github.com/notif-dev/notif/internal/httpapi"
	"github.com/notif-dev/notif/internal/web"
)

type AttachmentObjectStore = engine.AttachmentObjectStore

// ErrorStatus preserves retry classification without exposing engine internals.
func ErrorStatus(err error) int { return engine.ErrorStatus(err) }

type Config struct {
	DatabaseURL, Schema, PublicURL, AttachmentsDir string
	SecretKey                                      []byte
	MaxConnections                                 int32
	Attachments                                    AttachmentObjectStore
	AllowLocal                                     bool
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
	return &Service{store: s, api: httpapi.New(s, c.PublicURL, web.Handler()), public: c.PublicURL}, nil
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
	workers.Add(2)
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
	go func() {
		defer workers.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for ctx.Err() == nil {
			worked, err := s.store.ProcessAutomaticAnswer(ctx)
			if err != nil && ctx.Err() == nil {
				slog.Error("automatic answers failed", "error", err)
			}
			if worked && err == nil {
				continue
			}
			select {
			case <-ctx.Done():
			case <-tick.C:
			}
		}
	}()
	workers.Wait()
}
