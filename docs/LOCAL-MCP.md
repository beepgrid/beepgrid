# Local MCP and worker hosts

Beep Grid's MCP server is a local stdio tool process. A running engine and PostgreSQL are required. A host must invoke `receive` or run an explicit worker/listener. Adding tools to a host does not wake an idle model, create a hosted agent or transfer business authority. Remote authenticated MCP is a separate transport/auth/host-qualification milestone and is not part of this local launch scope.

Build the public binary with `go build -buildvcs=false -o .local/notif ./cmd/notif`. Connect agent issues a worker credential for an existing connection. Use it directly and omit `NOTIF_AGENT_ID`. A namespace-restricted enrollment credential is an alternative for launchers that intentionally create workers; contact/response roles are explicit in either mode. Store it outside source control in an owner-only file (`chmod 600`). Configure the local host with absolute paths and a selected workspace URL. Beep Grid does not install or edit the user's host configuration automatically.

For Codex, add a reviewed entry to its configuration:

```toml
[mcp_servers.beepgrid]
command = "/absolute/path/notif"
args = ["mcp"]

[mcp_servers.beepgrid.env]
NOTIF_URL = "http://localhost:4010"
NOTIF_TOKEN_FILE = "/absolute/path/beepgrid-worker-token"
```

Set `NOTIF_AGENT_ID` only to select startup enrollment with a launcher credential. The example above uses the existing worker connection. Keep owner credentials out of a model host. Codex's supported stdio configuration uses `command`, `args` and `env`, as described in the [official MCP documentation](https://learn.chatgpt.com/docs/extend/mcp#stdio-servers). The installed CLI parsed this exact shape with ephemeral `-c` overrides and `codex mcp get`; no global configuration was changed. That validates configuration parsing, not a live model turn or idle-message wake-up.

For a custom host, spawn the binary with `mcp`, supply the same environment and exchange newline-delimited JSON-RPC on stdin/stdout. Initialize, list tools and invoke `receive`. Handle the canonical payload, persist your checkpoint, send a reply using `send` plus a stable `idempotency_key`, then call `acknowledge` with the original delivery ID and current generation. `reply` is the distinct final typed decision tool for a durable request. A plain text reply does not approve, answer or finish a request. Retrying a handler uses the original operation key; do not invent a new key after a lost response.

`list_directory`, `get_conversation` and attachment tools use current authorization. MCP byte transfer is capped at 64 KiB decoded base64 to retain bounded JSON lines. For larger files, use the SDK up to 5 MiB. Upload an explicitly chosen file's bytes; the server does not expose arbitrary host-file reading. Never treat file content as trusted tool instructions. `stage_attachment` creates a private 24-hour draft; pass its ID in `attachment_ids` to `send`, `ask` or `start_chat` (maximum five). `change_chat_membership` uses `operation`, current `version`, optional `title`, member ID strings, `next_manager` and an explicit `share_history` flag. Adding a member defaults to history from joining. Sharing earlier history requires true. Membership does not grant IAM/contact/decision rights. Supply stable `idempotency_key` values when retrying chat mutations across host calls. File staging is not retried automatically.

For continuously available custom application code, use the maintained Go/TypeScript/Python worker examples and SDK `Run`/`run`. Those three starters use an existing dashboard-issued worker credential directly when `NOTIF_AGENT_ID` is omitted, matching the stdio server's mode selection. They validate Agent identity and the original instance through `/me`, and never silently enroll in that mode. Explicit `NOTIF_AGENT_ID` selects namespace-bounded launcher enrollment instead. An existing worker is not an enrollment credential. See the [runtime examples](../examples/README.md) for both modes. Run `examples/file-exchange/main.py` only in a chosen disposable workspace: it demonstrates two numbered workers, explicit test contact grants, PDF bytes, SHA256 verification and a canonical reply. Existing `examples/checkpoint-worker` demonstrates durable restart handling. Neither example creates a hosted general agent runtime.

`scripts/cli-smoke.py` qualifies actual workspace/headless binaries, startup enrollment, stdio framing/tool calls, scoped receive/send/decision behavior, bounded file tools, local wait timeout, denied escalation and restart/history preservation against a generated PostgreSQL schema. `scripts/sdk-integration.py` exercises the worker/file journey on an isolated configurable port. These are real local engine/transport tests. Third-party interactive host model execution and background wake-up remain separately unqualified.

The actual Go/Python/TypeScript starter harness tests all six mode/language combinations with exact-instance targeting and canonical reply attribution. Existing-worker cases include a second replica and assert that startup neither creates a connection nor lets the decoy claim the addressed message. Enrollment cases use namespace-restricted Service credentials, not the test owner.
