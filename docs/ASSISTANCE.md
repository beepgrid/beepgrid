# Optional assistance

Coordinator is off in every new workspace. An authorized person enables it in Settings. People, agents and services keep their existing identities. There is no privileged overseer account.

Configure a built-in model adapter or keep your own HTTP model gateway. No provider or model is selected by default. Your existing agents can use any runtime or model independently of this optional assistance configuration.

## Built-in model configuration

Set these server environment variables, then enable the desired features in Settings:

```sh
export NOTIF_AI_PROVIDER=anthropic
export NOTIF_AI_MODEL=YOUR_EXPLICIT_MODEL_ID
export NOTIF_AI_API_KEY=YOUR_PROVIDER_CREDENTIAL
```

These are illustrative placeholders. Supply secrets through your environment or secret manager, never a tracked file or browser bundle. No API keys are discovered from other provider variables. Neither model discovery nor a model request runs at startup. An incomplete configuration fails startup instead of selecting a model for you. Coordinator remains off until an authorized person enables it.

| Provider value | Native API / default base URL | Default output mode |
| --- | --- | --- |
| `openai` | Responses / `https://api.openai.com/v1` | `schema` |
| `anthropic` | Messages / `https://api.anthropic.com/v1` | `schema` |
| `google` | GenerateContent / `https://generativelanguage.googleapis.com/v1beta` | `schema` |
| `openai-compatible` | Chat Completions / explicit `NOTIF_AI_BASE_URL` required | `json` |

`NOTIF_AI_BASE_URL` is a base URL including the API version path, not the final operation URL. For example, a compatible endpoint at `http://127.0.0.1:11434/v1` receives POST requests at `/v1/chat/completions`. Specify your installed model's ID and set `NOTIF_AI_ALLOW_PRIVATE=true` to use an HTTP or private-network inference endpoint in production. This opt-in applies only to the configured AI endpoint; channel and legacy gateway address restrictions remain unchanged. Use HTTPS when sending credentials over a network. TLS certificate verification, connection-time DNS validation/pinning and redirect rejection remain enforced. Google model names may optionally start with `models/`.

One model configuration serves Coordinator planning and reply summarization. It adds no required runtime, database or proxy service. Native APIs use provider-specific authentication headers; Google credentials never appear in query strings. OpenAI response storage and background execution are disabled. The adapters never send tools, hosted-agent definitions or previous conversation references.

Applications embedding the public Go runtime set `runtime.Config.AI` with `runtime.AIConfig`. The optional `runtime.AIConfigFromEnv(os.Getenv)` helper reads exactly the same variables as the standalone binary. A library never reads environment variables implicitly. Models and workspace feature toggles remain explicit, and every tenant retains separate authorization and saved-plan visibility even when sharing a provider.

`NOTIF_AI_OUTPUT_MODE` explicitly selects `schema`, `json` or `text`. Schema mode requests native structured output. JSON mode requests JSON-object output from OpenAI/compatible endpoints or JSON MIME output from Google. Anthropic supports `schema` or `text`. Text mode supplies the schema in the prompt for models without native structured output. Every mode enforces the same local schema, byte bounds and engine authorization checks; there is no silent mode change, JSON repair or cross-provider failover. Unsupported model/API settings produce a failed assistance call, leaving the normal delivery and human response paths available. Valid JSON alone is not proof of a correct interpretation.

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

Use `NOTIF_OVERSEER_URL` / `NOTIF_OVERSEER_TOKEN` for an explicitly configured custom HTTP gateway. It overrides the built-in model for Coordinator planning and summarization. Gateway-only installations do not need `NOTIF_AI_*` variables. Native API credentials and gateway tokens remain separate. Engine credentials are never sent to the gateway. Production gateways use public-address validation, pinned dialing, HTTPS and redirect restrictions. Loopback development gateways require `serve --dev`.

The gateway accepts JSON POST requests and returns JSON with HTTP 200. Model I/O runs outside the workspace transaction. Context and output are bounded to 32 KiB, with at most two active calls, one call per person per second and 1000 calls per adapter per hour. Coordinator planning and summarization have 30-second deadlines. Failures never turn into authorization or fabricated success.

## Exact published answers

Informational answers can be explicitly published for an audience, scoped key and validity period. Reuse requires identical context, compatible choices and current requester, publisher and original responder authority. Publication does not grant the original answerer permission to answer another person's question. Invalidating a publication stops future reuse and marks historical consumers. This deterministic path does not call a model or delay alerts.

## Explicit Coordinator review

The enabled Agents page offers a natural-language interface and saved recent requests. It can read structured progress from permitted agents, prepare messages, ask fresh questions or request tasks. Reports remain labeled as worker-reported, with freshness and source details. It never infers completion from an acknowledgement or adds potentially overlapping counters.

`POST /api/v1/fleet/plan` accepts `{prompt,target?}`. The provider receives only agents the person may currently View, with a maximum of 100 agents and 24 KiB of source context. Select a group for larger fleets. The provider returns a proposal, never executes tools:

```json
{"operation":"question","agent_ids":["agt_example"],"body":"What have you completed?","summary":"Ask the permitted research agent for fresh progress."}
```

Supported operations are `status`, `message`, `question`, and `task`. The engine rejects any invented target or other operation. Outbound proposals expire after ten minutes and require explicit confirmation. `POST /api/v1/fleet/command` accepts only `{plan_id}`. It rechecks current View plus Message or Ask permission and original group membership, then sends the exact saved content atomically. There is one durable request per agent for questions/tasks. Retrying confirmation does not send twice. Approvals, tool execution and IAM changes are outside the overseer's capability.

`GET /api/v1/fleet/plans` lists the person's recent permitted plans. `GET /api/v1/fleet/plans/{id}` reloads one plan and its canonical replies. Agent replies separately require current permission to reply to the person. The person's permission to ask does not grant an agent permission to respond.

`POST /api/v1/fleet/plans/{id}/summary` sends authorized replies to the gateway with `task: overseer_summary`, `prompt`, `replies` and engine instructions. Each reply includes `agent_id`, `name`, `state`, optional `outcome` and `answer`. The only response field is `summary`. The explicit review flow can request a summary of settled replies; automatic coordination uses durable synthesis jobs described below. Both retain individual replies and expose provider failures. A digest caches summaries and prevents committing a summary when replies changed during the model call.

Every read, confirmation and summary is scoped to the original workspace and person. All context sources are rechecked, including sources that influenced the model but were not selected for outbound messages. Revocation, disabled people/agents, credential expiry, group changes, feature toggles and recovery holds fail closed. An already displayed response cannot be erased from a person's memory, but subsequent reads and actions enforce current authority. New assistance decisions are disabled during backup restore reconciliation.

Go, TypeScript and Python clients expose settings, planning, confirmation, saved-plan reads and reply summaries. Agent MCP `ask` supports `shared_key` for explicit exact-answer reuse. The overseer UI and SDKs act using a person's credential.

## Local verification

The normal Go tests exercise all four provider protocols through local HTTP servers and a real PostgreSQL database, including default-off configuration, explicit gateway precedence, confirmation replay and permission changes during model I/O. Provider fixtures are protocol tests, not evidence of live vendor access or identical model quality. `scripts/verify.sh` runs functional, SDK, UI, race and release checks. For an explicitly authorized local model run, `python3 scripts/codex-assistance-e2e.py` tests Coordinator using saved Codex ChatGPT authentication in an isolated schema. It rejects API-key authentication and does not copy authentication into the repo, container or public CI. The OSS runtime has no Codex dependency.

Add `--protocol openai-compatible` to qualify the built-in compatible adapter against a local endpoint backed by that same subscription. The harness also accepts `openai`, `anthropic` and `google` wire wrappers. These flags select local protocol emulation, not live vendor APIs or vendor credentials. The qualification script explicitly selects its recorded test model; the OSS engine still has no default model.

### Personal Overseer conversation

The workspace presents the optional assistant explicitly in Agents and Conversations. It is a built-in assistant surface, not a privileged Agent principal. Each Person's saved turns remain private. Ordinary conversations and group sends never invoke it implicitly.

`POST /api/v1/fleet/plan` accepts optional `previous_plan_id` to continue that Person's conversation. The configured planning adapter receives a `history` array containing at most six authorized turns and 12 KiB of prompt/interpretation/proposal/reply context. Proposals retain their confirmed/unconfirmed distinction. Current fleet sources alone determine eligible outgoing targets. Historical context cannot expand authority. Follow-up results retain inherited agent, group-membership and reply provenance, and recheck it on read, model completion and confirmation. Oversized accumulated provenance requires a new topic. No database migration is needed.

An optional `Idempotency-Key` gives browser planning submissions a durable retry identity. Replays return the same canonical turn after current authorization checks; changed input with the same key conflicts. Existing unkeyed SDK callers remain supported. `POST /api/v1/query` with `collection: "overseer_plans"` provides newest-first, person-bound chronological paging and prompt search. Opaque cursors are account/query scoped. The legacy recent-plan array retains its shape and limit.

## Automatic Coordinator dispatch and durable synthesis

`POST /api/v1/fleet/coordinate` is the default automatic coordination path. It accepts `prompt`, optional `target` and `previous_plan_id`, plus an optional stable Idempotency-Key. The bounded plan can contain different instructions per agent. The engine saves the turn, dispatches permitted ordinary messages/questions/tasks as the Person, collects canonical replies and uses durable summary jobs to synthesize the authorized results. Read the same saved plan to observe dispatches, individual replies, partial/failure state, `reply_summary` and `summary_error`. A provider failure does not fabricate completion or remove the individual results.

The earlier `/fleet/plan` and `/fleet/command` endpoints remain the explicit review path. Manual `/summary` remains an available retry/read path; automatic coordinate turns no longer require a person to press it after every result. Both paths retain current Person authority, source visibility, group provenance and revocation checks. No Coordinator service principal or business-tool execution authority is added. Hosted inference must be configured and independently qualified; local protocol fixtures and subscription-backed test harnesses do not prove a managed production provider is enabled.
