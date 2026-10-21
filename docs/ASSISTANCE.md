# Optional assistance

Automatic answer reuse and the overseer are off in every new workspace. An owner enables each independently in Settings. People, agents and services keep their existing identities. There is no privileged overseer account.

Configure your own HTTP model gateway with `NOTIF_AUTOMATIC_ANSWER_URL` / `NOTIF_AUTOMATIC_ANSWER_TOKEN` and `NOTIF_OVERSEER_URL` / `NOTIF_OVERSEER_TOKEN`. Tokens authenticate only to the gateway. Engine credentials are never sent to it. Production gateway URLs use the existing public-address validation, pinned dialing, HTTPS and redirect restrictions. Loopback development gateways require `serve --dev`.

The gateway accepts JSON POST requests and returns JSON with HTTP 200. It may use any model provider or local model. Model I/O runs outside the workspace transaction. Context and output are bounded to 32 KiB, with at most two active calls per configured adapter, one call per person per second and 1000 calls per adapter per hour. Automatic matching has a 20-second deadline; overseer planning/summarization have 30 seconds. Failures never turn into authorization or fabricated success.

## Automatic informational answers

1. An agent asks an ordinary question with an explicit `shared_key`, such as `india-pricing/2026/policy-v1`.
2. A person answers, then explicitly publishes that answer for a chosen audience and validity period. The existing exact-match reuse behavior remains available without a model or toggle.
3. If automatic reuse is enabled, a differently worded question with the same key and choices gets a durable matching job. Only active published answers whose audience includes the requester become candidates. Approval requests and tasks never enter this path.
4. A sufficiently confident equivalent match is applied through normal response authorization. The engine rechecks the requester credential, author/publisher rights, current group eligibility, audience, expiry, request state and settings after the model returns. The result includes its source ID, automatic method and explanation. Invalidated sources remain visibly marked on historical results.

Request to matching gateway:

```json
{
  "task": "automatic_answer_match",
  "instructions": "...engine-supplied matching rules...",
  "request": {"title": "Should amounts include GST?", "body": "India report", "scope_key": "india-pricing/2026/policy-v1", "choices": []},
  "candidates": [{"id": "ans_example", "question": "Use tax-inclusive prices?", "scope_key": "india-pricing/2026/policy-v1"}]
}
```

Response:

```json
{"answer_id":"ans_example","equivalent":true,"confidence":0.995,"reason":"Same informational question and policy version."}
```

The engine accepts only a supplied candidate ID, `equivalent: true`, confidence in [0.98, 1], and a bounded explanation. Ambiguity, conflicting meanings, invented IDs, timeouts and provider failures leave the question open for its human recipients. Semantic confidence is a model estimate, not proof; use narrow versioned keys and conservative publications. Never use shared informational answers as permission to perform a business action.

External alerts wait up to 25 seconds for matching, then recover independently. Native history is immediate. Jobs use expiring leases and a fencing token so a stale worker cannot apply a second result. Disabling assistance invalidates pending model decisions and releases delayed alerts. No public inbound endpoint or separate queue service is required.

Manual reviewed suggestions retain their separate `NOTIF_MATCHER_URL` / `NOTIF_MATCHER_TOKEN` configuration and existing `{request,candidates,instructions}` request with `{suggestions:[{answer_id,reason}]}` response. You may use the same gateway if it supports both contracts; automatic calls have an explicit `task` field. Configuring either adapter does not silently change the other.

## Overseer

The enabled Agents page offers a natural-language interface and saved recent requests. It can read structured progress from permitted agents, prepare messages, ask fresh questions or request tasks. Reports remain labeled as worker-reported, with freshness and source details. It never infers completion from an acknowledgement or adds potentially overlapping counters.

`POST /api/v1/fleet/plan` accepts `{prompt,target?}`. The provider receives only agents the person may currently View, with a maximum of 100 agents and 24 KiB of source context. Select a group for larger fleets. The provider returns a proposal, never executes tools:

```json
{"operation":"question","agent_ids":["agt_example"],"body":"What have you completed?","summary":"Ask the permitted research agent for fresh progress."}
```

Supported operations are `status`, `message`, `question`, and `task`. The engine rejects any invented target or other operation. Outbound proposals expire after ten minutes and require explicit confirmation. `POST /api/v1/fleet/command` accepts only `{plan_id}`. It rechecks current View plus Message or Ask permission and original group membership, then sends the exact saved content atomically. There is one durable request per agent for questions/tasks. Retrying confirmation does not send twice. Approvals, tool execution and IAM changes are outside the overseer's capability.

`GET /api/v1/fleet/plans` lists the person's recent permitted plans. `GET /api/v1/fleet/plans/{id}` reloads one plan and its canonical replies. Agent replies separately require current permission to reply to the person. The person's permission to ask does not grant an agent permission to respond.

`POST /api/v1/fleet/plans/{id}/summary` sends authorized replies to the gateway with `task: overseer_summary`, `prompt`, `replies` and engine instructions. Each reply includes `agent_id`, `name`, `state`, optional `outcome` and `answer`. The only response field is `summary`. The UI summarizes settled replies automatically, retaining individual replies and retrying only at the person's request after a provider failure. A digest caches summaries and prevents committing a summary when replies changed during the model call.

Every read, confirmation and summary is scoped to the original workspace and person. All context sources are rechecked, including sources that influenced the model but were not selected for outbound messages. Revocation, disabled people/agents, credential expiry, group changes, feature toggles and recovery holds fail closed. An already displayed response cannot be erased from a person's memory, but subsequent reads and actions enforce current authority. New assistance decisions are disabled and pending jobs cancelled during backup restore reconciliation.

Go, TypeScript and Python clients expose settings, planning, confirmation, saved-plan reads and reply summaries. Agent MCP `ask` already supports `shared_key`; automatic reuse needs no agent-specific tool or model dependency. The overseer UI and SDKs act using a person's credential.

## Local verification

The normal Go tests exercise a real PostgreSQL database with deterministic, bounded provider fixtures, including permission changes during model I/O. `scripts/verify.sh` runs functional, SDK, UI, race and release checks. For an explicitly authorized local model run, `python3 scripts/codex-assistance-e2e.py` uses saved Codex ChatGPT authentication in an isolated schema. It rejects API-key authentication and does not copy authentication into the repo, container or public CI. The OSS runtime has no Codex dependency.
