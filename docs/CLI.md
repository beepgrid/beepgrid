# Command-line and agent usage

The same `beepgrid` executable is a server, an HTTP client and an MCP stdio adapter. Both workspace and headless builds include these commands. They require a running engine and PostgreSQL; the CLI never implements a second permission or persistence layer.

Run `beepgrid` or `beepgrid help` for command syntax and local setup guidance. Client flags precede positional arguments. Client JSON results go to stdout; client command errors are JSON on stderr. Server database connection/configuration failures print a short setup message on stderr and exit before the workspace starts. Exit codes are 0 success, 1 other failure, 2 invalid usage/input, 3 authentication/authorization failure and 4 interrupted/deadline-exceeded. Server worker logs remain ordinary operational logs. API conflict and validation details retain the server's error code and HTTP status.

## Authenticate without a browser

Start the engine as described in [packaging](PACKAGING.md). For a local development checkout:

```sh
export BEEPGRID_URL=http://localhost:4010
export BEEPGRID_TOKEN_FILE=.local/owner-token
.local/beepgrid api GET /api/v1/me
.local/beepgrid seed --prefix research/ --out .local/research-launcher-token
```

`seed` is an explicit trusted administrator setup action. It grants the launcher a bounded namespace and an owner-only contact role. Administrators can change the audience with the same role/binding API or workspace Permissions page. An agent's name and metadata never confer authority.

Enrollment input, saved as `enroll.json`:

```json
{"agent":"research/scout","name":"Scout","capabilities":["message","request"]}
```

```sh
BEEPGRID_TOKEN_FILE=.local/research-launcher-token .local/beepgrid enroll --input enroll.json --out .local/scout-token
export BEEPGRID_TOKEN_FILE=.local/scout-token
```

The new token file is mode 0600 and never overwritten. stdout contains agent/instance metadata and the file path, never the credential. Each enrollment creates an independent instance credential; reuse the saved token until it expires rather than enrolling on every command. Rotate/re-enroll deliberately when needed. If the network response is lost, inspect existing instances or revoke unused credentials before retrying enrollment.

Client commands and MCP require `BEEPGRID_TOKEN_FILE` or `BEEPGRID_TOKEN` explicitly. `BEEPGRID_TOKEN` takes precedence when both are set. `seed`, `trace` and `diagnose` retain the local owner's convenience fallback. Never give the owner token to an untrusted agent or assume a local installation grants administrative consent.

## Questions and durable results

Save this as `question.json`, substituting an authorized person's ID from administrator setup:

```json
{"target":{"kind":"person","id":"PERSON_ID"},"title":"Which report format?","choices":["Brief","Detailed"]}
```

```sh
.local/beepgrid ask --input question.json --key report-42/format/v1
.local/beepgrid wait --timeout 5m REQUEST_ID
```

Store the returned request ID and stable key in your job checkpoint. A retry with the same key and identical input recovers the accepted request. `wait` reads until a terminal state; a deadline or Ctrl+C never approves, cancels or marks execution successful. Read the returned state and outcome, including cancelled/expired results.

The authorized person can answer in the workspace or through the CLI. Save `response.json` using the current request version:

```json
{"outcome":"answered","answer":"Brief","version":1}
```

```sh
BEEPGRID_TOKEN_FILE=.local/owner-token .local/beepgrid respond --input response.json REQUEST_ID
```

`approve --input` and `task --input` use the same request schema with kind `approval` and `task`. `ask` accepts the request schema's explicit kind too. Optional bound action approvals require the complete action descriptor and current policy; approval is not proof of execution. Use the [SDK/MCP execution flow](INTEGRATIONS.md) to redeem and report protected actions.

`send agent|person|group ID MESSAGE` remains available. `send --input file.json --key stable-key` accepts the SDK message schema, including `conversation_id` for replies. Use explicit stable keys for recoverable creation operations. An omitted key generates a fresh operation and cannot recover a previous command after a restart.

## Read and manage through the same API

```sh
.local/beepgrid list --limit 50 agents
.local/beepgrid list --limit 50 --cursor SAVED_CURSOR requests
.local/beepgrid api GET /api/v1/requests/REQUEST_ID
.local/beepgrid api --input group.json POST /api/v1/groups
.local/beepgrid api --input role.json POST /api/v1/roles
.local/beepgrid api --input binding.json POST /api/v1/role-bindings
```

`list` returns `items`, `order` and an optional `next_cursor`. Continue with that cursor, preserving the collection/search options. `api` accepts relative `/api/v1/` JSON routes from [OpenAPI](../api/openapi.json); no separate CLI-specific management semantics exist. It also supports groups, permission checks, worker inbox/acknowledgements and other JSON endpoints. Binary downloads and streaming events use the HTTP API/SDK directly.

`--input -` reads stdin. Inputs are limited to 4 MiB. `--timeout` bounds the entire call (30 seconds by default, five minutes for `wait`). `--out FILE` saves a JSON result to a new protected file and prints only its path. Credential/session issuance through `api` requires `--out`; prefer `enroll` for a token file directly usable by clients. Treat arbitrary API output files as sensitive. No implicit retry key is added to generic API mutations; provide `--key` where the API supports idempotency.

## MCP for coding agents

Configure an MCP stdio server with executable `.local/beepgrid`, argument `mcp`, and environment `BEEPGRID_URL` plus `BEEPGRID_TOKEN_FILE` pointing at a scoped worker token. There is no model billing or hosted account prerequisite for this adapter. Tools use the same engine authorization as HTTP/CLI/workspace operations.

Alternatively, use a namespace-scoped enrollment credential plus `BEEPGRID_AGENT_ID` and optionally `BEEPGRID_AGENT_NAME` to enroll when MCP starts. This creates a fresh instance per start. Persisted worker credentials are preferable when the caller needs explicit instance lifecycle control. See [examples](../examples/README.md) for delivery checkpoints and the integration contract for request/response semantics.

## Workspace files

`beepgrid files CONVERSATION_ID` lists current visible files. `beepgrid file-upload --conversation CONVERSATION_ID --media application/pdf ./brief.pdf` uploads chosen bytes to an existing conversation. `beepgrid file-download --out ./brief-copy.pdf ATTACHMENT_ID` writes a new owner-only file and returns a JSON receipt; it refuses to overwrite an existing path. Flags precede positional arguments. Transfers are bounded to 5 MiB. Upload does not automatically retry an unknown response; inspect the file list before repeating. All operations enforce current conversation/history access, and file content remains untrusted.
