# Capability and benchmark coverage

This is an implementation/evidence matrix, not a claim that all design targets or commercial work are complete. The current workstream is the open source release. Maintained enterprise directory sync, managed cloud, billing and marketing are separate workstreams. Comparative local measurements are kept in a separate benchmark repository; they do not establish a production capacity or hosting-cost claim.

Every scenario's cold/warm end-user setup time is **not measured by unfamiliar users**. The quickstart is reproducible and the scripts record automated drills, but a maintainer's successful command is not a usability study. A release candidate should get an independent fresh-install journey before public promotion.

| # | Scenario | Current support and evidence | Remaining boundary |
| --- | --- | --- | --- |
| 01 | Existing database self-host | Go binary + embedded UI, PostgreSQL, clean install/upgrade tests, Docker build | Qualify an operator's actual database/network/backup policy |
| 02 | Import 300 employees/groups | Manual People, verified OIDC JIT, department/manager, explicit group/role APIs | Maintained directory sync is enterprise work; OIDC is not directory import |
| 03 | Programmatic approval | Go/Python/TS HTTP tests, durable typed request and exact permit | Business-tool effect enforcement is customer/runtime integration |
| 04 | Unseen agent | Namespace enrollment, identity/instance creation and UI directory | No claimed arbitrary runtime auto-interruption |
| 05 | 10,000 workers | Distinct credentials, instance attribution, bounded limits, wakeup hints | 10,000-worker throughput/cost/soak not measured; directory lists 1,000 |
| 06 | Contact running agent | Actual browser-to-Builder reply and SDK worker tests | Runtime handler must process input |
| 07 | One-way personal update | Directional roles and default-deny tests | A room's read visibility is provider-governed |
| 08 | Multiple conversations | Independent canonical IDs, private history and Slack thread fixture | Live Slack account still requires validation |
| 09 | Private group notification | Per-recipient conversation projection and isolation tests | Explicit fan-out limit 1,000 |
| 10 | Shared-room destination coalescing | Explicit destinations and private fan-out supported | Automatic cross-recipient shared-room coalescing deferred; no unsafe approximation |
| 11 | Primary/fallback routes | Primary/copy/fallback modes, actual HTTP temporary/permanent/Unknown tests | Provider-specific live behavior/quotas |
| 12 | Agent-to-agent tasks | Common target/policy/request transport, competing replica tests | Runtime executes its own task handler; no general workflow orchestrator |
| 13 | Readers/questioners/approvers | Signature/account/action/connection checks, amount conditions, unauthorized callback tests | Live workspace account/scope validation |
| 14 | Offboarding while active | Disabled identities, credential/role/group revocation, stream/replay and permit rechecks | Automatic directory reconciliation and unfinished-work reassignment deferred |
| 15 | Simultaneous group decisions | Real PostgreSQL concurrent first-response tests | External effects still need idempotency/fencing |
| 16 | Worker crash while awaiting | Restartable checkpoint adapter, saved handle, duplicate result prevention | Other runtime adapters need their own checkpoint support |
| 17 | Ask now/wait later | Same durable handle, SDK wait cancellation tests | Cannot keep an uncheckpointed process alive |
| 18 | Several pending questions | Independent requests, focused queue, drafts, clarification/UI tests | Channel-native controls differ; web is the full receiving surface |
| 19 | Exact scoped duplicates | Stacked UI, suppressed duplicate external attention, atomic answer/share and independent handles | Explicit publication/audience required; no inferred approval reuse |
| 20 | Paraphrase questions | Removed from current product | No model matching or suggestions |
| 21 | Reuse prior answer | Explicit audience/provenance/expiry, invalidation and current publisher checks | Source-system changes require explicit invalidation/versioning |
| 22 | Replace proposal | Cancel/supersede, exact descriptor/version, stale response rejection | Re-proposal creates a new request; no external undo |
| 23 | Offline worker | Timed presence, durable queue, expiry, logical/exact-instance targets | Large offline backlog/soak capacity not measured |
| 24 | Selected fleet | Directory search by trusted owner/project/parent, explicit groups, production resolver preview | Dynamic selector language deferred; preview does not send |
| 25 | Fleet progress | Permission-filtered structured status, fresh/stale sources and partial coverage | Cached worker-reported status; no exact overlap-blind sums or arbitrary private-tool queries |
| 26 | Overseer commands | Safely returns CAPABILITY_UNSUPPORTED; direct structured group tasks work | Natural-language command translation deferred |
| 27 | Provider failure | HTTP temporary rejection, Retry-After, permanent failure, no automatic Unknown replay and fallback tests | SMTP now distinguishes safe pre-acceptance/4xx retry, permanent 5xx rejection, ambiguous final responses and confirmed acceptance. Bounded independent destination lanes preserve order. Live contracts remain account-specific |
| 28 | Engine/DB recovery | Engine and PostgreSQL container restart, worker reconnect, transactional inbox/outbox, real dump/restore reconciliation drill | Production RPO/RTO and post-snapshot external reconciliation not measured |
| 29 | Lost event stream | Durable cursors, hidden-page advancement, current authorization and browser reconnect | Not a guarantee against every browser/provider outage |
| 30 | Store conformance/upgrade | PostgreSQL 17, checksummed v1-to-current migration, concurrent migrators, race suite | SQLite and database-to-database migration unsupported |

## Measured local evidence

- Real PostgreSQL Go integration/race suite covers identity isolation, policies, requests, concurrent decisions, leases, private history, providers, OIDC, shared answers, checkpoint adapter and CLI seed.
- Clean quickstart and non-root container smoke scripts validate fresh databases, two agents, a reply, cookie login and history across restart. These are maintainer-run checks.
- The SDK harness builds a separate server/schema, migrates it, runs thirteen TypeScript and seven Python tests with real HTTP, and cleans only its generated schema.
- The dashboard/React suites cover separate directories, question focus, clarification, group review, stale fleet coverage, response denials and embeds.
- The dump/restore drill uses generated test databases and a simulated external effect after the snapshot. Old credentials are revoked, approvals superseded and re-execution denied. Its report is `.local/recovery-drill.json`; local automated timing excludes manual business reconciliation and is not a production RTO.
- Thirty local server qualification trials used up to 25 separate authenticated SDK worker processes and verified 3,890 replies/receipts, including jitter, lost committed responses, signed provider failures and real lease-expiry replacement. [Results and limitations](SERVER-PERFORMANCE.md) separate deterministic fleet performance from real Codex model checks; they are not a production capacity or hosting-cost guarantee.
- `govulncheck` is run with the pinned toolchain, and npm audit/dependency notices cover the installed locked dependencies. Scans are point-in-time checks, not a security guarantee.

## Launch gates outside automated local implementation

Select the public repository/package namespace and a private security-reporting contact before publication. Local Mailpit SMTP was exercised end to end. Validate production Slack/OIDC/SMTP against the deployment's real accounts, permissions and policies. Have an unfamiliar developer repeat the documented clean install and failure recovery. Run and publish capacity/cost measurements before claiming a fleet size or provider reliability SLA. Configure long-term retention/backups and review operational exposure for the deployment.

The commercial product and website are not represented as complete by this open source checklist. The excluded speculative mini-apps, Try both and demonstration learning are not in either current phase.

## Adversarial implementation audit

See [the item-by-item audit](PHASE1-AUDIT.md) for all 85 checklist entries and [local performance measurements](PERFORMANCE.md) for raw samples and qualifications. The audit fixed current-authority, dispatch-lock, stale UI and SDK response-limit defects missed by earlier suites. Two actual Codex workers passed seven model turns with saved ChatGPT subscription authentication, canonical instance attribution, peer tasks, human clarification/decisions, an exact permit and one verified local fixture effect. This is a finite integration test, not a continuous model-hosting service.

## Byte and memory qualification

The [byte and memory follow-up](BYTE-MEMORY-PERFORMANCE.md) freezes engine `1c55efe`: all 225 current-engine timed trials passed in a 443-trial matrix, with 80 native Linux tests, nine capability gates, the full functional/release suite, eleven harness tests, the production image and two real Codex/MCP agents. A common fan-out body crosses each bounded SQL batch once, private receipts share the body, and large full public receipts stream with exact JSON compatibility. Optional compact acknowledgements and a resumable TypeScript event consumer reduce body traffic while retaining full replay and existing interfaces.

All losing metrics remain published. Linux-local SMTP controls distinguish native transport from host-forwarding tails; rejected fixture setup results are excluded explicitly. Temporary-rejection latency is not compared as equal work when successful message counts differ. The dashboard sender uses compact acceptance; its incoming path still uses summary hints and debounced refresh. Full-event traffic gains require using the SDK iterator. Two-worker headroom gates, native ARM64 versus compile-only amd64, provider-account and deployment-capacity limits remain explicit.
