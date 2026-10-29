# Unreleased

Development namespace and versions remain pending a public release decision. No registry publication is implied.

30 October 2026: message-first examples, clean packed-consumer checks and explicit runtime/qualification limits. TypeScript, Go and Python support current-access workspace files and canonical event consumption. React embeds continue to use same-origin authenticated Person sessions and typed durable decisions. MCP file tools are limited to 64 KiB; SDK files to 5 MiB. Shared membership and initial-message attachment contracts follow the separately owned engine work.

The Go/TypeScript/Python StartChat helper now accepts the main engine's optional initial `mentions` field with Unicode code-point offsets. All shared members still receive the message. Older engines reject that additive input field; use the integrated 30 October engine contract before sending it.

Runtime starter correction: omitted NOTIF_AGENT_ID now uses the supplied existing worker credential and original instance directly. Explicit IDs still select bounded launcher enrollment. Real HTTP checks cover both modes in Go/Python/TypeScript, exact-instance reply attribution, unchanged replica count and decoy isolation.
