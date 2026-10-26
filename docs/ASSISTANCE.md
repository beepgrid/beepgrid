# Optional assistance

Automatic answer reuse and the overseer are off in every new workspace. An owner enables each independently in Settings. People, agents and services keep their existing identities. There is no privileged overseer account.

Configure a built-in model adapter or keep your own HTTP model gateway. No provider or model is selected by default. Your existing agents can use any runtime or model independently of this optional assistance configuration.

## Built-in model configuration

Set these server environment variables, then enable the desired features in Settings:

```sh
export NOTIF_AI_PROVIDER=anthropic
export NOTIF_AI_MODEL=YOUR_EXPLICIT_MODEL_ID
export NOTIF_AI_API_KEY=YOUR_PROVIDER_CREDENTIAL
```

These are illustrative placeholders. Supply secrets through your environment or secret manager, never a tracked file or browser bundle. No API keys are discovered from other provider variables. Neither model discovery nor a model request runs at startup. An incomplete configuration fails startup instead of selecting a model for you. Both workspace toggles remain off until the owner enables them. Manual suggestions still require an explicit reviewer request.

| Provider value | Native API / default base URL | Default output mode |
| --- | --- | --- |
| `openai` | Responses / `https://api.openai.com/v1` | `schema` |
| `anthropic` | Messages / `https://api.anthropic.com/v1` | `schema` |
| `google` | GenerateContent / `https://generativelanguage.googleapis.com/v1beta` | `schema` |
| `openai-compatible` | Chat Completions / explicit `NOTIF_AI_BASE_URL` required | `json` |

`NOTIF_AI_BASE_URL` is a base URL including the API version path, not the final operation URL. For example, a compatible endpoint at `http://127.0.0.1:11434/v1` receives POST requests at `/v1/chat/completions`. Specify your installed model's ID and set `NOTIF_AI_ALLOW_PRIVATE=true` to use an HTTP or private-network inference endpoint in production. This opt-in applies only to the configured AI endpoint; channel and legacy gateway address restrictions remain unchanged. Use HTTPS when sending credentials over a network. TLS certificate verification, connection-time DNS validation/pinning and redirect rejection remain enforced. Google model names may optionally start with `models/`.

One model configuration serves automatic matching, reviewed suggestions, overseer planning and reply summarization. It adds no required runtime, database or proxy service. Native APIs use provider-specific authentication headers; Google credentials never appear in query strings. OpenAI response storage and background execution are disabled. The adapters never send tools, hosted-agent definitions or previous conversation references.

Applications embedding the public Go runtime set `runtime.Config.AI` with `runtime.AIConfig`. The optional `runtime.AIConfigFromEnv(os.Getenv)` helper reads exactly the same variables as the standalone binary. A library never reads environment variables implicitly. Models and workspace feature toggles remain explicit, and every tenant retains separate authorization and saved-plan visibility even when sharing a provider.

`NOTIF_AI_OUTPUT_MODE` explicitly selects `schema`, `json` or `text`. Schema mode requests native structured output. JSON mode requests JSON-object output from OpenAI/compatible endpoints or JSON MIME output from Google. Anthropic supports `schema` or `text`. Text mode supplies the schema in the prompt for models without native structured output. Every mode enforces the same local schema, byte bounds and engine authorization checks; there is no silent mode change, JSON repair or cross-provider failover. Unsupported model/API settings produce a failed assistance call, leaving the normal delivery and human response paths available. A valid JSON response alone is not proof of a correct semantic match.

Use `NOTIF_AI_OPTIONS` for a JSON object of provider generation settings. They are passed unchanged, except for engine-owned formatting, model selection and context fields. Unknown or protected settings fail configuration rather than being silently discarded.

| Provider | Supported settings |
| --- | --- |
| OpenAI | `max_output_tokens`, `temperature`, `top_p`, `service_tier`, `reasoning` (`effort`, `summary`), `text.verbosity` |
| Anthropic | `max_tokens`, `temperature`, `top_p`, `top_k`, `thinking` (`type`, `budget_tokens`), `output_config.effort` |
| Google | `maxOutputTokens`, `temperature`, `topP`, `topK`, `seed`, `thinkingConfig` (`thinkingBudget`, `thinkingLevel`, `includeThoughts`), `stopSequences` |
| Compatible | `max_tokens`, `max_completion_tokens`, `temperature`, `top_p`, `top_k`, `min_p`, `seed`, `frequency_penalty`, `presence_penalty`, `reasoning_effort`, `reasoning` (`effort`, `summary`, `enabled`, `max_tokens`), `thinking` (`type`, `budget_tokens`), `repeat_penalty`, `stop` |

For example, `NOTIF_AI_OPTIONS='{"reasoning":{"effort":"high"},"max_output_tokens":8192}'` configures an OpenAI model that supports those settings. Token limits default to 4096 and can be set between 1 and 65536. API/model capability varies; setting a supported configuration field does not guarantee every model accepts it. Thinking budgets must fit within the existing call deadlines. The options object is limited to 8 KiB. Network envelopes are limited to 256 KiB, while the actual model result remains limited to 32 KiB. Truncation, refusal, unexpected tool content, duplicate JSON keys, missing/extra output fields and ambiguous multi-candidate responses fail closed. Provider body/error text and credentials are excluded from diagnostics. Each adapter call performs one request without application retries.

The implementation uses Go's HTTP/JSON stack behind the existing bounded assistance interface. Vercel AI SDK provides this pattern for TypeScript, but would require a JavaScript service here. The native adapters keep the existing Go deployment and avoid an additional dependency/runtime. Protocol references: [OpenAI structured outputs](https://developers.openai.com/api/docs/guides/structured-outputs), [Anthropic structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs), [Google GenerateContent](https://ai.google.dev/api/generate-content), [Google structured output](https://ai.google.dev/gemini-api/docs/structured-output), [DeepSeek JSON mode](https://api-docs.deepseek.com/guides/json_mode/), [Vercel AI SDK](https://ai-sdk.dev/docs/introduction).

## Existing custom gateways

Use `NOTIF_AUTOMATIC_ANSWER_URL` / `NOTIF_AUTOMATIC_ANSWER_TOKEN`, `NOTIF_OVERSEER_URL` / `NOTIF_OVERSEER_TOKEN` or `NOTIF_MATCHER_URL` / `NOTIF_MATCHER_TOKEN` for the original HTTP contracts. Each explicitly configured URL overrides the built-in model for only that feature; an overseer URL covers planning and summarization. Unspecified gateway URLs leave the built-in model in place. Gateway-only installations do not need any `NOTIF_AI_*` variables. Native API credentials and gateway tokens remain separate. Tokens authenticate only to the gateway. Engine credentials are never sent to it. Production gateway URLs use the existing public-address validation, pinned dialing, HTTPS and redirect restrictions. Loopback development gateways require `serve --dev`.

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

This is not all-pairs request clustering. The automatic worker handles only open informational questions with an explicit reuse key, prefilters at most eight published answers by workspace/key/choices/requester audience, and skips the model when none qualify. The current runtime runs one matching-job loop per engine process. A slow matcher can accumulate queued jobs; the 25-second external-alert hold remains a product tradeoff even if matching eventually fails. Model confidence is self-reported and is not a calibrated accuracy guarantee. Keep this optional path disabled when immediate external alerts take priority.

Exact reuse also rechecks the original answerer's current ability to read and respond to the new request, including recipient/group eligibility and reply permission. Publishing an answer to an agent audience does not turn its author into another person's authorized responder. This applies both when publication visits existing waiters and when a future identical question is created. A rejected reuse leaves that question open for its intended recipient.

Manual reviewed suggestions retain their separate `NOTIF_MATCHER_URL` / `NOTIF_MATCHER_TOKEN` configuration and existing `{request,candidates,instructions}` request with `{suggestions:[{answer_id,reason}]}` response. You may use the same gateway if it supports both contracts; automatic calls have an explicit `task` field. Configuring either adapter does not silently change the other.

## Overseer

The enabled Agents page offers a natural-language interface and saved recent requests. It can read structured progress from permitted agents, prepare messages, ask fresh questions or request tasks. Reports remain labeled as worker-reported, with freshness and source details. It never infers completion from an acknowledgement or adds potentially overlapping counters.

`POST /api/v1/fleet/plan` accepts `{prompt,target?}`. The provider receives only agents the person may currently View, with a maximum of 100 agents and 24 KiB of source context. Select a group for larger fleets. The provider returns a proposal, never executes tools:

```json
{"operation":"question","agent_ids":["agt_example"],"body":"What have you completed?","summary":"Ask the permitted research agent for fresh progress."}
```

Supported operations are `status`, `message`, `question`, and `task`. The engine rejects any invented target or other operation. Outbound proposals expire after ten minutes and require explicit confirmation. `POST /api/v1/fleet/command` accepts only `{plan_id}`. It rechecks current View plus Message or Ask permission and original group membership, then sends the exact saved content atomically. There is one durable request per agent for questions/tasks. Retrying confirmation does not send twice. Approvals, tool execution and IAM changes are outside the overseer's capability.

`GET /api/v1/fleet/plans` lists the person's recent permitted plans. `GET /api/v1/fleet/plans/{id}` reloads one plan and its canonical replies. Agent replies separately require current permission to reply to the person. The person's permission to ask does not grant an agent permission to respond.

`POST /api/v1/fleet/plans/{id}/summary` sends authorized replies to the gateway with `task: overseer_summary`, `prompt`, `replies` and engine instructions. Each reply includes `agent_id`, `name`, `state`, optional `outcome` and `answer`. The only response field is `summary`. The UI offers an explicit summary action for settled replies, retains individual replies and leaves retries to the person after a provider failure. A digest caches summaries and prevents committing a summary when replies changed during the model call.

Every read, confirmation and summary is scoped to the original workspace and person. All context sources are rechecked, including sources that influenced the model but were not selected for outbound messages. Revocation, disabled people/agents, credential expiry, group changes, feature toggles and recovery holds fail closed. An already displayed response cannot be erased from a person's memory, but subsequent reads and actions enforce current authority. New assistance decisions are disabled and pending jobs cancelled during backup restore reconciliation.

Go, TypeScript and Python clients expose settings, planning, confirmation, saved-plan reads and reply summaries. Agent MCP `ask` already supports `shared_key`; automatic reuse needs no agent-specific tool or model dependency. The overseer UI and SDKs act using a person's credential.

## Local verification

The normal Go tests exercise all four provider protocols through local HTTP servers and a real PostgreSQL database, including default-off flags, provenance, independent budgets, explicit gateway precedence, confirmation replay and permission changes during model I/O. Provider fixtures are protocol tests, not evidence of live vendor access or identical model quality. `scripts/verify.sh` runs functional, SDK, UI, race and release checks. For an explicitly authorized local model run, `python3 scripts/codex-assistance-e2e.py` uses saved Codex ChatGPT authentication in an isolated schema. It rejects API-key authentication and does not copy authentication into the repo, container or public CI. The OSS runtime has no Codex dependency.

Add `--protocol openai-compatible` to qualify the built-in compatible adapter against a local endpoint backed by that same subscription. The harness also accepts `openai`, `anthropic` and `google` wire wrappers. These flags select local protocol emulation, not live vendor APIs or vendor credentials. The qualification script explicitly selects its recorded test model; the OSS engine still has no default model.

### Personal Overseer conversation

The workspace presents the optional assistant explicitly in Agents and Conversations. It is a built-in assistant surface, not a privileged Agent principal. Each Person's saved turns remain private. Ordinary conversations and group sends never invoke it implicitly.

`POST /api/v1/fleet/plan` accepts optional `previous_plan_id` to continue that Person's conversation. The configured planning adapter receives a `history` array containing at most six authorized turns and 12 KiB of prompt/interpretation/proposal/reply context. Proposals retain their confirmed/unconfirmed distinction. Current fleet sources alone determine eligible outgoing targets. Historical context cannot expand authority. Follow-up results retain inherited agent, group-membership and reply provenance, and recheck it on read, model completion and confirmation. Oversized accumulated provenance requires a new topic. No database migration is needed.

An optional `Idempotency-Key` gives browser planning submissions a durable retry identity. Replays return the same canonical turn after current authorization checks; changed input with the same key conflicts. Existing unkeyed SDK callers remain supported. `POST /api/v1/query` with `collection: "overseer_plans"` provides newest-first, person-bound chronological paging and prompt search. Opaque cursors are account/query scoped. The legacy recent-plan array retains its shape and limit.
