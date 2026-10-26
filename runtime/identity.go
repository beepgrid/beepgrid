package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/notif-dev/notif/internal/engine"
)

// VerifiedPerson is an immutable identity already verified by a trusted host.
// These methods are in-process composition primitives, never public HTTP login
// endpoints. A host must authorize workspace membership before calling them.
type VerifiedPerson struct {
	Issuer, Subject, Name, Email string
}

type PersonSession struct {
	Token     string    `json:"-"`
	PersonID  string    `json:"person_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func validIdentity(in VerifiedPerson) bool {
	return engine.ValidText(in.Issuer, 2048) && engine.ValidText(in.Subject, 512) && engine.ValidText(in.Name, 120) && len(in.Email) <= 320
}

// ProvisionIdentityWorkspace binds the first owner once. A replay may update
// display fields, but cannot replace the original owner with another identity.
// The bootstrap credential remains server-side; its full authority is deliberate.
func (s *Service) ProvisionIdentityWorkspace(ctx context.Context, id, company, bootstrap string, owner VerifiedPerson) error {
	if !validIdentity(owner) {
		return fmt.Errorf("invalid verified owner")
	}
	if err := s.ProvisionWorkspace(ctx, id, company, bootstrap); err != nil {
		return err
	}
	return s.store.BindVerifiedOwner(ctx, id+"/production", bootstrap, owner.Issuer, owner.Subject, owner.Name, owner.Email)
}

// EstablishPersonSession never gives a newly admitted Person administrator
// authority. Only an existing deliberately bound bootstrap owner has that status.
func (s *Service) EstablishPersonSession(ctx context.Context, id string, in VerifiedPerson) (PersonSession, error) {
	if !workspaceID.MatchString(id) || !validIdentity(in) {
		return PersonSession{}, fmt.Errorf("invalid verified person")
	}
	token, err := s.store.LoginOIDC(ctx, id+"/production", in.Issuer, in.Subject, in.Name, in.Email)
	if err != nil {
		return PersonSession{}, err
	}
	a, err := s.store.Authenticate(ctx, token)
	if err != nil {
		return PersonSession{}, err
	}
	return PersonSession{Token: token, PersonID: a.ID, ExpiresAt: time.Now().Add(12 * time.Hour)}, nil
}

// PersonSessionScope is a host's path/session fence, not a replacement for the
// authorization checks performed by the engine handler on each operation.
func (s *Service) PersonSessionScope(ctx context.Context, token, id string) error {
	a, err := s.store.Authenticate(ctx, token)
	if err != nil {
		return err
	}
	if a.Kind != "person" || a.CredentialKind != "browser" || a.Scope != id+"/production" {
		return engine.Denied()
	}
	return nil
}

func (s *Service) RevokePersonSession(ctx context.Context, token string) error {
	a, err := s.store.AuthenticateLogout(ctx, token)
	if err != nil {
		return err
	}
	return s.store.Logout(ctx, a)
}

// CheckProgrammaticCredential prevents a trusted host from exposing its browser
// session credentials through an unbound SDK API route. Ordinary engine handlers
// still perform their full authorization checks after this admission fence.
func (s *Service) CheckProgrammaticCredential(ctx context.Context, token string) error {
	a, err := s.store.Authenticate(ctx, token)
	if err != nil {
		return err
	}
	if a.CredentialKind == "browser" {
		return engine.Denied()
	}
	return nil
}
