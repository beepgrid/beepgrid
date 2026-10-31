# Capabilities and verification

This file summarizes the public engine's boundaries. User-facing instructions and method references are at [beepgrid.com/docs](https://beepgrid.com/docs/). Checked-in API and SDK inventories are under [developer](developer/).

| Capability | Supported behavior | Boundary |
| --- | --- | --- |
| Identity and access | Separate people, agents and services; enrollment, worker instances, directional permissions, revocation and delegated administration | Conversation membership controls disclosure and does not grant IAM or external tool authority |
| Conversations | Direct and shared chats, explicit membership/history sharing, structured mentions and message attachments | Private fanout creates separate conversations; mentions do not narrow a shared audience |
| Requests and decisions | Informational questions, typed approvals, cancellation/replacement, idempotency and exact execution permits | A permit authorizes an intent; a runtime must enforce and reconcile the external effect |
| Durable transport | Leased inboxes, acknowledgements, reconnects, retained request handles and resumable event cursors | Applications retain their own execution checkpoints; provider acceptance is distinct from delivery |
| Coordinator | Private follow-up conversations, scoped specialist selection, provider-neutral model configuration and programmatic APIs | Optional model configuration is required; no arbitrary runtime hosting or implied tool permissions |
| Shared answers and status | Explicitly scoped answer publication/reuse and permission-filtered worker-reported fleet state | No semantic answer matching; reported status is not independently observed execution |
| Channels | Workspace plus documented Slack, Discord, Telegram, SMTP and webhook integration paths | Account permissions and live provider behavior require separate qualification; WhatsApp is deferred |
| Developer interfaces | Go, TypeScript, Python, React, HTTP API, CLI and MCP | Package identifiers remain source-compatible; registry availability must be checked separately |
| Deployment | PostgreSQL, bundled workspace, headless builds, non-root containers, explicit migrations, export and recovery tooling | Operators choose their backup, retention, network and provider-account policies |

## What normal verification covers

`bash scripts/verify.sh` runs Go vet and race tests in both build modes, workspace and React tests, real HTTP SDK integration, executable developer tours, API/method inventory checks, dependency audits and binary/CLI smoke checks. PostgreSQL is real; model responses in the documentation harness are deterministic fixtures.

`scripts/sdk-package-check.py` verifies packed TypeScript/React consumers, a Python wheel in a fresh environment and an external Go module. `scripts/container-smoke.py` checks a built image's non-root user, fresh migration, authentication and workspace/headless behavior. `scripts/recovery-drill.py` exercises database snapshot restoration and reconciliation with disposable fixture state.

These checks do not certify a live provider account, production capacity or an availability SLA. See [live channel qualification](LIVE-CHANNEL-QUALIFICATION.md), [channel recovery](CHANNEL-RECOVERY-QUALIFICATION.md) and [Coordinator recovery](COORDINATOR-RECOVERY-QUALIFICATION.md) for explicitly bounded evidence.

## Performance evidence

Retained measurements include [server profiling](SERVER-PERFORMANCE.md), [portable delivery qualification](PORTABLE-PERFORMANCE.md), [byte and memory qualification](BYTE-MEMORY-PERFORMANCE.md), [workspace event traffic](DASHBOARD-LIVE-PERFORMANCE.md) and later component reports under [benchmarks](benchmarks/).

Historical source SHAs in those reports identify the original measured trees. The [source revision map](benchmarks/SOURCE-REVISIONS.md) points to the corresponding cleaned history. Repository history cleanup does not turn those measurements into results for a newer commit. Losing cases and failure dispositions remain available in [the failure audit](FAILURE-AUDIT.md). Local timing, compile-only architecture checks and finite provider probes are labeled separately from production guarantees.
