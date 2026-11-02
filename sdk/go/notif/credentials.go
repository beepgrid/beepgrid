package notif

import (
	"context"
	"net/url"
	"time"
)

// Credential is metadata only. Permissions restrict, never grant, identity access.
type Credential struct {
	ID                 string     `json:"id"`
	PrincipalID        string     `json:"principal_id"`
	Name               string     `json:"name"`
	OwnerName          string     `json:"owner_name"`
	Kind               string     `json:"kind"`
	Prefix             string     `json:"prefix"`
	TokenHint          string     `json:"token_hint"`
	CreatedAt          time.Time  `json:"created_at"`
	LastUsedAt         *time.Time `json:"last_used_at"`
	ExpiresAt          *time.Time `json:"expires_at"`
	RenewableUntil     *time.Time `json:"renewable_until"`
	PreviousUntil      *time.Time `json:"previous_until"`
	Permissions        []string   `json:"permissions"`
	Version            int64      `json:"version"`
	Revoked            bool       `json:"revoked"`
	ParentCredentialID string     `json:"parent_credential_id,omitempty"`
}
type CredentialInput struct {
	PrincipalID string     `json:"principal_id"`
	Kind        string     `json:"kind"`
	Name        string     `json:"name,omitempty"`
	Prefix      string     `json:"prefix,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Permissions []string   `json:"permissions,omitempty"`
}
type CredentialEdit struct {
	Version          int64      `json:"version"`
	Name             *string    `json:"name,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	Permissions      *[]string  `json:"permissions,omitempty"`
	ClearPermissions bool       `json:"clear_permissions,omitempty"`
}
type IssuedCredential struct {
	Token     string    `json:"token"`
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	ExpiresAt time.Time `json:"expires_at"`
}
type CredentialReplacement struct {
	Token      string     `json:"token"`
	Credential Credential `json:"credential"`
}

func (c *Client) Credentials(ctx context.Context) (out []Credential, err error) {
	err = c.Do(ctx, "GET", "/api/v1/credentials", nil, &out, "")
	return
}
func (c *Client) CreateCredential(ctx context.Context, in CredentialInput) (out IssuedCredential, err error) {
	err = c.Do(ctx, "POST", "/api/v1/credentials", in, &out, "")
	return
}
func (c *Client) EditCredential(ctx context.Context, id string, in CredentialEdit) (out Credential, err error) {
	err = c.Do(ctx, "POST", "/api/v1/credentials/"+url.PathEscape(id)+"/edit", in, &out, "")
	return
}

// RotateCredential returns a secret once. It is deliberately not automatically retried.
func (c *Client) RotateCredential(ctx context.Context, id string, version int64, overlapSeconds int) (out CredentialReplacement, err error) {
	err = c.Do(ctx, "POST", "/api/v1/credentials/"+url.PathEscape(id)+"/rotate", map[string]any{"version": version, "overlap_seconds": overlapSeconds}, &out, "")
	return
}
func (c *Client) RevokeCredential(ctx context.Context, id string, revokeWorkers bool) error {
	return c.Do(ctx, "POST", "/api/v1/credentials/"+url.PathEscape(id)+"/revoke", map[string]bool{"revoke_workers": revokeWorkers}, nil, "")
}
func (c *Client) RenewCredential(ctx context.Context) (out Credential, err error) {
	err = c.Do(ctx, "POST", "/api/v1/credentials/self/renew", map[string]any{}, &out, "")
	return
}
