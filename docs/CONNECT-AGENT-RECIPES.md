# Connect Agent: minimal supported setup

For live input during an existing Agent task, choose a harness/framework in the
workspace and follow [Live input](LIVE-INPUT.md). The sequential worker and
tool-only paths below remain available under Advanced.

The dashboard creates one worker connection and supplies its credential. Use that worker credential directly; omit `NOTIF_AGENT_ID`. Do not ask the developer to create a Service, launcher or second Agent for this path. Select the URL of the same workspace and store the credential in an owner-only file or the runtime's secret manager.

## Common environment

```sh
export NOTIF_URL="https://your-workspace.example"
export NOTIF_TOKEN_FILE="/absolute/path/beepgrid-worker-token"
chmod 600 "$NOTIF_TOKEN_FILE"
```

The starter validates `/api/v1/me` as an Agent with an instance ID, prints that identity and runs receive/reply. It never silently enrolls. A successful API write confirms canonical queueing; use an actual reply to prove the running process.

## Choose one runtime

Run these from the OSS repository root:

| Selection | Supported command | Import for custom code |
| --- | --- | --- |
| Go | `go run -buildvcs=false ./examples/go-worker` | `github.com/beepgrid/beepgrid/sdk/go/notif` |
| Python | `PYTHONPATH=sdk/python python3 examples/python-worker/main.py` | `from notif import Client` |
| TypeScript / Node | `npm --prefix sdk/typescript ci && npm --prefix sdk/typescript run build`, then `node examples/typescript-worker/main.mjs` | `import { Client } from '@notif/client'` in a packaged consumer |

Custom code constructs `notif.New(url, workerToken)` in Go or `Client(url, workerToken)` in Python/TypeScript, then calls `Run`/`run` with its handler. The maintained TypeScript starter imports the built repository SDK by relative path. Python can alternatively install the local package with `python3 -m pip install ./sdk/python`.

Package names `@notif/client`, `notif-client` and the Go repository path are development placeholders until the public release decision. These commands work from source. Do not show a registry install command as available before publication. Local packed artifacts and external consumers are separately checked by `scripts/sdk-package-check.py`.

## Local MCP host

Build `go build -buildvcs=false -o .local/notif ./cmd/notif`, then configure the host to spawn the absolute binary with argument `mcp` and the same two environment variables. For Codex:

```toml
[mcp_servers.beepgrid]
command = "/absolute/path/notif"
args = ["mcp"]

[mcp_servers.beepgrid.env]
NOTIF_URL = "https://your-workspace.example"
NOTIF_TOKEN_FILE = "/absolute/path/beepgrid-worker-token"
```

The host initializes, lists tools and calls `receive`; it sends through `send` and acknowledges only after handling the delivery. This is a local stdio process. Adding tools alone does not wake an idle model. Remote authenticated MCP and third-party model execution are separately qualified work.

## Connection proof and advanced options

The live runtime prints the exact Agent and instance ID from its original worker token. The six Go/Python/TypeScript starter cases test direct-worker and explicit-enrollment modes over real HTTP, including exact-instance delivery, canonical reply attribution, unchanged replica count and a decoy replica that cannot claim the addressed message. CLI and local MCP run as actual binaries against disposable PostgreSQL state. No hosted model runtime is inferred.

Only show enrollment instructions when intentionally selected: a namespace-restricted launcher credential plus `NOTIF_AGENT_ID` enrolls a new worker. `NOTIF_AGENT_NAME` alone does not select enrollment.

The new SDK file and membership helpers add no connection setup fields. They expose private `stageAttachment`/`stage_attachment`/`StageAttachment`, `attachment_ids` (`AttachmentIDs` in Go), and versioned `changeChatMembership`/`change_chat_membership`/`ChangeChatMembership`. Earlier history is shared only with explicit `share_history: true`. The dashboard guide can keep these out of the initial connection steps.
