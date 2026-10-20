package engine

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

// LoginOIDC accepts only claims already verified by the configured OIDC verifier.
// It never merges users by email or turns a source group into approval authority.
func (s *Store) LoginOIDC(ctx context.Context, scope, issuer, subject, name, email string) (string, error) {
	if !ValidText(subject, 512) || !ValidText(issuer, 2048) {
		return "", Bad("A verified OIDC subject is required.")
	}
	external := "oidc/" + Digest(issuer+"\x00"+subject)
	a := Actor{Scope: scope}
	var token string
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		p, e := scanPrincipal(tx.QueryRow(ctx, `SELECT `+principalFields+` FROM principals WHERE scope=$1 AND kind='person' AND external_id=$2`, scope, external))
		if e != nil && ErrorStatus(e) != 404 {
			return e
		}
		if e != nil {
			if name == "" {
				name = "Team member"
			}
			p = Principal{ID: ID("per"), Kind: "person", ExternalID: external, Name: name}
			if _, e = tx.Exec(ctx, `INSERT INTO principals(id,scope,kind,external_id,name,metadata) VALUES($1,$2,'person',$3,$4,$5)`, p.ID, scope, external, name, JSON(map[string]string{"oidc_issuer": issuer, "oidc_subject": subject, "email": email, "source": "oidc"})); e != nil {
				return e
			}
		}
		if p.Disabled {
			return Denied()
		}
		a.Principal = p
		token, _, e = issue(ctx, tx, a, p.ID, "browser", "", 12*time.Hour)
		if e != nil {
			return e
		}
		return event(ctx, tx, a, "person.sso_login", "", p.ID, map[string]any{"id": p.ID, "issuer": issuer})
	})
	return token, err
}
func (s *Store) LinkOIDC(ctx context.Context, a Actor, id, issuer, subject string) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		if !a.Admin {
			return Denied()
		}
		if !ValidText(subject, 512) || !ValidText(issuer, 2048) {
			return Bad("issuer and subject are required.")
		}
		tag, e := tx.Exec(ctx, `UPDATE principals SET external_id=$3,metadata=metadata||$4::jsonb WHERE scope=$1 AND id=$2 AND kind='person'`, a.Scope, id, "oidc/"+Digest(issuer+"\x00"+subject), JSON(map[string]string{"oidc_issuer": issuer, "oidc_subject": subject}))
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return Missing()
		}
		return event(ctx, tx, a, "person.oidc_linked", "", id, map[string]any{"issuer": issuer})
	})
}
func (s *Store) CreateService(ctx context.Context, a Actor, external, name string) (Principal, error) {
	p := Principal{ID: ID("svc"), Kind: "service", Name: name, ExternalID: external, Metadata: map[string]any{}, CreatedAt: time.Now()}
	if !ValidText(name, 120) || !ValidText(external, 200) {
		return p, Bad("Name and external_id are required.")
	}
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !a.Admin {
			return Denied()
		}
		if _, e := tx.Exec(ctx, `INSERT INTO principals(id,scope,kind,external_id,name) VALUES($1,$2,'service',$3,$4)`, p.ID, a.Scope, external, name); e != nil {
			return e
		}
		return event(ctx, tx, a, "service.created", "", p.ID, map[string]string{"id": p.ID})
	})
	return p, e
}
