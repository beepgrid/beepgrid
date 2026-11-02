# 0.1.1

The setup command and workspace setup prompt now install the published npm or Python SDK directly into the existing Agent project. Source checkouts are optional, and the prompt retains credential-file handling, runtime-specific guidance and verification steps.

# 0.1.0

First public release of the Beepgrid SDK, compatible with the API v1 engine.

Canonical packages: `@beepgrid/sdk`, `@beepgrid/react` and Python `beepgrid`. Python retains the `notif` import as a compatibility alias.

30 October 2026: message-first examples, clean packed-consumer checks and explicit runtime/qualification limits. TypeScript, Go and Python support current-access workspace files and canonical event consumption. React embeds continue to use same-origin authenticated Person sessions and typed durable decisions. MCP file tools are limited to 64 KiB; SDK files to 5 MiB. Private draft uploads, attachment IDs on initial messages/requests, attachment projections and shared membership helpers now follow the committed schema 32 engine contract. Earlier-history sharing defaults to false; removal revokes file/history access. TypeScript empty conversation history is optional, matching JSON. Local MCP adds stage_attachment, start_chat and change_chat_membership, and CLI adds file-stage.

The Go/TypeScript/Python StartChat helper now accepts the main engine's optional initial `mentions` field with Unicode code-point offsets. All shared members still receive the message. Older engines reject that additive input field; use the integrated 30 October engine contract before sending it.

Runtime starter correction: omitted NOTIF_AGENT_ID now uses the supplied existing worker credential and original instance directly. Explicit IDs still select bounded launcher enrollment. Real HTTP checks cover both modes in Go/Python/TypeScript, exact-instance reply attribution, unchanged replica count and decoy isolation.

Atomic pending-message replacement is available through TypeScript `replaceRequest`, Python `replace_request` and Go `ReplaceRequest`, plus the local MCP `replace_request` tool. Stable keys, current versions and preserved audiences/files require the integrated engine endpoint. A replacement always needs a fresh decision.
