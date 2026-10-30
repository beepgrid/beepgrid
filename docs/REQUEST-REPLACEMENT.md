# Replace a pending message

The sender can use **Edit and resend** on an open question or approval. The old message remains in history as Replaced. Its replacement has a fresh request ID and needs a fresh response. A completed decision cannot be edited this way.

`POST /api/v1/requests/{id}/replace` requires an `Idempotency-Key` and the current `version`, new `body`, and optional code-point `mentions`. It returns `previous` and `replacement`. Keep the same key and payload when retrying an unknown result; changing the payload with that key returns a conflict. A replay returns the currently authorized request state.

Replacement commits atomically with superseding the old request and cancelling its queued deliveries. It preserves the original recipients, private conversation projections, exact runtime target, files, choices, expiry, blocking declaration and any bound action. It never copies an approval or shared-answer key. Current contact permissions, file access and shared-chat membership are checked again. A missing recipient or revoked permission fails the whole operation and leaves the old request open. New role/team/chat members do not silently join the old audience.

Go `ReplaceRequest`, TypeScript `replaceRequest`, Python `replace_request` and the local MCP `replace_request` tool follow the same contract. This is a communication operation. Any customer-controlled execution still uses its own current authority and idempotency boundary.

Real PostgreSQL tests cover decision/replacement races, stale decisions, replay, private projections, exact instances, files, bound-action redemption, current permissions and member removal. Real HTTP SDK tests exercise all three clients. Dashboard tests preserve the edited draft after a conflict or unknown result.
