# Access tokens

Manage tokens in **Settings → Access tokens**, or use the same HTTP API and Go,
TypeScript or Python SDKs headlessly. No hosted control plane or extra service is
required. People, Agents and software services keep separate identities.

## Choose an identity and a lifetime

- **API access:** acts with its owner's current permissions. Optionally restrict
  the token further to read, message, request, respond, execute or manage actions.
  A limit never grants permissions the identity lacks. Restricted API tokens
  cannot issue other credentials or turn into browser sessions.
- **Agent enrollment:** a launcher can connect Agents only under its allowed
  external-key prefix, for example `research/`. Keep it out of the running Agent.
- **Agent connection:** identifies one worker instance. Use its existing token
  directly with any SDK, CLI or MCP connection. Do not enroll again on each call.

New API and enrollment tokens default to 30 days. Choose an expiry from one
minute to one year ahead, or select **Never expires**. The edit form always shows the saved expiry in the **Expires** dropdown. For renewable connections this is their hard deadline, not the shorter access lease. Saving other fields keeps that expiry unchanged. Existing tokens keep their current lifetime. Tokens
have names, a non-secret hint, creation time and last-use information. Last use
is recorded at most once per five minutes, so it is approximate.

Secrets are generated randomly, returned once and stored only as SHA-256 digests.
Listings, queries and audit events never return a secret or its digest. Store
the secret outside source control, in a private file or secret manager. Use TLS
outside local development.

### Never-expiring keys

**Never expires** is available directly in the expiry selector, including when
editing an active key. In Agent connection setup it is an option under
**Connection security → Connection valid for**. Such connections remain valid
while idle and need no automatic renewal.

Headless callers send `never_expires: true` when creating a token, enrolling a
connection or editing a credential with its current `version`. Omit
`expires_at`; for connections also leave `renewable` false. The response and
metadata use `expires_at: null`. Go SDK response timestamps use `IsZero()` for
this case. Choosing a dated expiry later is supported. Rotation still has a
bounded old-secret overlap; revocation, disabled identities and all permission
checks continue to apply. An expired or revoked key cannot be revived this way.

## Keep an Agent connected

Opt into `renewable: true` when enrolling a connection. Its access lease lasts
24 hours and renews during authenticated activity when fewer than six hours
remain. Renewal is capped by `expires_at`, the hard connection deadline. With
renewal enabled and no deadline supplied, that deadline defaults to 30 days.
The workspace connection form defaults to a renewable 90-day connection and
exposes the lifetime under **Connection security**.

Renewal keeps the same secret, credential ID and instance ID. Messages, pending
requests, delivery leases and runtime sessions continue using that instance.
The model does not need to call a refresh tool or acknowledge anything extra.
All existing SDKs benefit because renewal is performed by the engine on normal
authenticated traffic, including event streams. There is no refresh daemon.

An idle connection that passes its 24-hour lease, a revoked token or a connection
past its hard deadline cannot renew itself. Reconnect deliberately. Before the
hard deadline, an authorized manager can extend it through the versioned edit
endpoint. Non-renewable enrollments preserve the previous 24-hour default.

```python
worker, session = launcher.connect(
    "research/scout", "Scout", renewable=True,
    connection_name="Production scout",
    expires_at="2027-02-01T00:00:00Z",
)
```

Use a future date within one year of issuance. The TypeScript `connect` helper
accepts the same fields as its fifth options argument. Go uses `EnrollInput`.

## Restrict an application token

```typescript
const issued = await admin.createCredential({
  principal_id: service.id,
  kind: "api",
  name: "Read-only reports",
  permissions: ["read"],
  expires_at: new Date(Date.now() + 30 * 86400000).toISOString(),
});
// Store issued.token privately. Do not print it or place it in a prompt.
```

Omit `permissions` to use the owner's identity access. `read` includes authorized
GETs and read-only query/preview operations. `message` sends messages, starts
chats and uploads attachments; `request` creates and changes requests;
`respond` answers or approves; `execute` redeems approvals and reports execution;
`manage` permits resource-management operations that the identity can perform.
Token management requires an unrestricted credential with the relevant identity
authority. New write endpoints default to requiring `manage` until classified.
Use `clear_permissions: true` in an authorized edit to restore identity access.

## Rotate and revoke

**Rotate** replaces a secret while preserving its identity, permissions and
expiry. Select a switch-over overlap: immediately, five minutes, one hour or up
to 24 hours. The default is one hour. The previous secret stops working at the
earlier of that deadline or its original expiry, including continuing streams.
Only the latest predecessor can overlap. A later rotation retires any older
predecessor immediately. Revoking the credential stops both secrets.

Save the replacement securely and update the runtime's token file or secret
configuration before the overlap ends. An in-memory SDK client must reload the
secret or be recreated; the runtime's existing instance ID must be reused.
Rotation does not extend expiry or replace an Agent instance.

Creation and rotation return secrets once and are not automatically retried.
If the response is lost, refresh the metadata before acting: revoke a newly
issued key you could not save, or rotate again with the current version while
the previous secret still works. Do not assume a timed-out write failed.

Revoking an enrollment token normally stops new enrollments only. Select **Also
stop connections issued by this token**, or pass `revoke_workers: true`, to
revoke its recorded worker descendants too. Lineage is recorded for enrollments
made after this feature was installed; disabling the Agent blocks all of its
connections, including legacy ones. Revocation never deletes conversation
history or reverses an action already executed by an external system.

## API

| Operation | Endpoint |
| --- | --- |
| List permitted metadata | `GET /api/v1/credentials` |
| Issue a token | `POST /api/v1/credentials` |
| Edit name, expiry or API limits | `POST /api/v1/credentials/{id}/edit` |
| Rotate with bounded overlap | `POST /api/v1/credentials/{id}/rotate` |
| Revoke, optionally with issued workers | `POST /api/v1/credentials/{id}/revoke` |
| Explicitly renew your current worker lease | `POST /api/v1/credentials/self/renew` |

Edit and rotate take the current metadata `version`; a stale version conflicts.
Expired or revoked tokens cannot be revived by these operations. Administrator
bootstrap credentials and browser sessions are managed through their existing
operator and login flows, rather than the ordinary token editor. Browser
sessions remain 12 hours; bootstrap credentials have no default expiry.

The SDK helpers are `credentials`, `createCredential`, `editCredential`,
`rotateCredential`, `revokeCredential` and `renewCredential` in TypeScript, with
corresponding snake-case Python and PascalCase Go methods. The CLI's `api GET` and
`api POST` commands can call the same endpoints. See the generated OpenAPI and SDK
reference for exact request fields.
