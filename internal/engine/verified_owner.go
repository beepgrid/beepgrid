package engine

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// BindVerifiedOwner is an in-process provisioning primitive. Binding is atomic
// with the scope lock, so two first-login claims cannot replace each other.
func (s *Store) BindVerifiedOwner(ctx context.Context, scope, bootstrap, issuer, subject, name, email string) error {
	a, err := s.Authenticate(ctx, bootstrap)
	if err != nil {
		return err
	}
	if a.Scope != scope || a.CredentialKind != "admin" || a.Kind != "person" {
		return Denied()
	}
	if !ValidText(issuer, 2048) || !ValidText(subject, 512) || !ValidText(name, 120) || len(email) > 320 {
		return Bad("Invalid verified owner.")
	}
	expected := "oidc/" + Digest(issuer+"\x00"+subject)
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		p, e := principal(ctx, tx, scope, a.ID)
		if e != nil {
			return e
		}
		if p.ExternalID != "owner" && p.ExternalID != expected {
			return Conflict("The workspace already has a different owner identity.")
		}
		if _, e = tx.Exec(ctx, `UPDATE principals SET external_id=$3,name=$4,metadata=metadata||$5::jsonb WHERE scope=$1 AND id=$2`, scope, p.ID, expected, name, JSON(map[string]string{"oidc_issuer": issuer, "oidc_subject": subject, "email": email})); e != nil {
			return e
		}
		return event(ctx, tx, a, "person.verified_owner_bound", "", p.ID, map[string]string{"id": p.ID, "issuer": issuer})
	})
}
