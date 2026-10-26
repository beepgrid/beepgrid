# API, SDKs and integrations

The machine-readable contract is [api/openapi.json](../api/openapi.json), also served at `/openapi.json`. Run `python3 scripts/openapi.py` after changing the contract; a Go test verifies registered literal routes are present. Authentication fixes workspace/environment scope. Sending a body with another scope or sender does not change it.

## Ordinary application code and agent runtimes

Use a service API credential for a conventional application. Use a namespace-restricted enrollment credential only in a trusted launcher. The first `connect`/`Enroll` creates/binds a logical Agent and returns a separate instance credential; workers use that credential thereafter. People and agents are never created through the same public collection. `/me` returns `person`, `agent` or `service` according to the authenticated kind.

A new `POST /api/v1/messages` with a target creates a conversation. Reply with the returned `conversation_id`. Repeating the same participants does not force the same conversation. `POST /api/v1/requests` creates a question, approval or task. SDK `wait` polls the same durable handle; cancellation of waiting is local. An advisory question can become blocking simply by waiting later.

Supply a stable `Idempotency-Key` for message/request creation. Retry the same key with the same body after uncertain network results. A conflicting body returns 409. Persist keys/handles with the runtime's job checkpoint. Generated SDK defaults protect a single network operation's retries, but a process restart needs an application-stable key.

Logical-agent targeting assigns work to one eligible replica. Use `target.kind=instance` for a particular instance. Use separate stable agent IDs per run/job when context must not be shared across replicas. Existing runtimes must handle their own model state, checkpoints, input processing and tool boundaries. A socket connection cannot make arbitrary code safely interruptible.

Each worker delivery carries an ID and lease generation. Handle durably, then acknowledge. A receipt does not assert business execution. Long polling uses an outbound HTTP connection; no public bridge or development tunnel is required. Acknowledgements from expired/replaced leases are rejected. Go/Python/TypeScript listeners reconnect on network/5xx/429 failures and surface authentication failures for renewal.

## Approvals and action enforcement

An approval binds `name`, `executor_id`, `resource`, `resource_version`, `arguments` and `operation_key`. The proposer needs Ask rights for the audience and the exact executor/action. A responder needs View, eligibility and Decide; being in a Slack channel grants no authority. Rules can constrain resource/version, currency, minor-unit amount ceiling and time. Redemption checks current rights and the unchanged descriptor.

The executor redeems through `/requests/{id}/redeem`, retrieves the same execution intent after retry, and reports through `/executions/{id}`. Enforce the permit at the actual business tool, and use the operation key/version to prevent external duplicates. The server does not execute your business action. `succeeded` is explicitly worker-reported, not an independently verified fact. Different operations cannot reuse an executor's operation key.

## Web and React

The built-in employee app uses HttpOnly sessions and durable event cursors through server-sent events. Lost SSE connections reconnect; timed refresh keeps presence/deadlines current. Current authorization is checked again during streaming/replay. Revoked access does not return with an old cursor.

The React package offers authenticated request/conversation components and a headless request hook. Keep the app and API behind the same origin/proxy and establish a real Person session. Do not embed owner, provider or enrollment tokens in frontend code. See [the package instructions](../sdk/react/README.md).

## MCP

Run `notif mcp` as an stdio server with `NOTIF_URL` and `NOTIF_TOKEN`. A worker token acts as that instance. Alternatively supply a launcher token plus `NOTIF_AGENT_ID`/`NOTIF_AGENT_NAME` for startup enrollment. Stdout is JSON-RPC only; logs go to stderr.

Tools include `list_agents`, `send`, `ask`, `get_request`, `reply`, `request_status`, `receive`, `acknowledge` and `report_progress`. Calls use the caller's existing permissions. The legacy initialize handshake advertises protocol `2025-11-25`; tool calls also work without a handshake. Remote Streamable HTTP MCP is outside this release. A host must call receive/poll or provide a listener; the stdio adapter cannot wake an arbitrary dormant model by itself.

## Slack

1. Create a Slack application/installation and obtain the bot token and signing secret. Grant only the provider scopes needed for the chosen channels and subscriptions.
2. Add a Slack connection with its immutable `team_id`. The UI stores bot/signing secrets encrypted. Set an optional `default_agent_id` for human-initiated conversations.
3. Configure Slack interactivity to `/integrations/slack/CONNECTION_ID`. Configure events to `/integrations/slack/CONNECTION_ID/events`; the endpoint handles signed URL verification. Subscribe to the relevant app mentions and direct messages.
4. Link immutable Slack user+team IDs to People using `POST /people/{id}/slack-account`. Email matching is never automatic. Assign View/Message/Decide roles separately.
5. Add the person's destination using the Slack channel ID. Incoming verified conversations also retain their own origin thread without changing the person's global destinations.

Messages map to canonical conversation threads. Bot/subtype messages are ignored to prevent echo loops. Interactive approval buttons carry only request/version references, not authority. Signatures, timestamp freshness, account links, audience and current connection/action rights are required. Duplicate callbacks/events are deduplicated. Someone else in a room cannot reply to a private conversation merely because they can see it.

Slack room visibility remains Slack's responsibility. Use direct/private destinations for sensitive content. Posting personalized messages to the same room does not automatically coalesce them; explicit shared-room coalescing is deferred. The implementation is covered by protocol fixtures and a stub Slack transport; connecting a live workspace remains an account-specific validation step.

## Email

Add an SMTP connection with `smtp_addr` including port, `from`, optional `username` and password. A password is required only when a username is configured; an unauthenticated local relay needs no artificial signing secret. STARTTLS is required outside `--dev`, and private addresses are blocked unless explicitly using local development. Destinations are valid recipient email addresses. A successful SMTP transaction is recorded as provider acceptance; ambiguous failures are Unknown.

Email decisions open `/requests/REQUEST_ID` and require a Person login and current rights. The URL itself is not a bearer approval. Inbound email parsing and email-based identity proof are outside this release. For local testing use Mailpit at `localhost:1125` with the browser at `localhost:8125`.

## Signed webhooks

Outgoing webhooks POST canonical public payloads. Internal policy/cluster metadata is removed. Headers are `X-Notif-Event-ID`, `X-Notif-Timestamp` and `X-Notif-Signature`. Compute the signature as HMAC-SHA256 over `v1:TIMESTAMP:` followed by the exact raw body, prefixed `v1=`. Verify constant-time and reject timestamps older than five minutes or more than one minute in the future. Dedupe the event ID before any downstream effect.

Incoming `/integrations/webhook/CONNECTION_ID` uses the same signature headers and a SendInput body. The connection's administrator-bound `inbound_sender_id` must be a Service. The body cannot select a sender. Its role controls whom it may contact. The event ID is required; identical replay returns the prior result, while changed content with the same ID returns 409. For richer questions/actions use the authenticated REST/SDK interface directly.

Provider URLs are HTTPS and publicly routable outside local development. DNS is validated again at connect, addresses are pinned by the dialer, redirects are refused and responses are bounded. Definite temporary rejection receives bounded retry with Retry-After and jitter. Unknown sends require reconciliation, not automatic fallback.

## Exact shared questions and Coordinator

An explicit `shared_key` identifies a resource/policy/version domain. Exact matching includes title, body and choices. Compatible pending informational questions are stacked in the web inbox, and redundant external attention waits behind the oldest matching open question for that person. Every requester retains its own ID, private conversation and cancellation/expiry state. When a leading question ends, the next independent question can become visible externally.

Answering one question does not silently answer the others. An authorized publisher may explicitly answer/share with the displayed audience for 24 hours, atomically, or publish an existing informational answer with a chosen audience/expiry. Approval permissions are never merged. Reuse preserves provenance; invalidation stops future reuse and signals historical consumers.

Fleet status collects permitted structured progress and timestamps. It reports partial coverage and never blindly adds overlapping counts. Coordinator is a separate opt-in private conversation that prepares bounded messages, questions and tasks for explicit confirmation. Ordinary group conversations never invoke it silently. See [ASSISTANCE.md](ASSISTANCE.md).

A group-target grant also matches its current members for individual contact. Removing membership removes that path immediately, including sensitive responses to pending requests. `$self` in a role target means the authenticated identity's own ID, so a launcher can grant a future worker authority to propose its own action without knowing the generated agent ID. A separate Ask grant still controls the human/group it may contact. The local seed command grants only owner contact plus self-proposal; it does not grant authority over other agents.

MCP `send` and `ask` accept an optional `idempotency_key`. Save and reuse it across agent/tool retries and runtime restarts. Omitting it creates a new logical operation on each tool call.

All three SDKs expose approval redemption and execution reporting (`Redeem`/`redeem` and `ReportExecution`/`reportExecution`/`report_execution`). MCP exposes `redeem_approval` and `report_execution`. These calls return authorization and reported evidence, and do not run arbitrary business tools. The protected tool must enforce the exact resource version and operation key.

### Workspace replacement contracts

The paged `POST /api/v1/query` endpoint accepts `conversation_id` for `requests`, as well as for `messages` and `attachments`. Request rows remain independently authorized; supplying a conversation ID does not grant access to any request or another tenant's data.

For `POST /api/v1/groups`, omitting `members` (or sending null) on an existing versioned group changes only its name and preserves its current roster. Send an explicit empty array to request removing all members, subject to current membership and role-assignment checks. A name-only edit returns the caller's filtered roster, with `members_complete=false` when appropriate. Keep the observed positive `version` to prevent stale writes.

`POST /api/v1/auth/logout` can revoke a valid Person browser session during workspace suspension. This exception provides no business-data or account-status access. Normal workspace admission checks continue to apply to other routes; account status retains its separate explicit authorization.
