# Connect live input to your existing Agent

Keep your model, tools and runtime. Add a receiver that can accept input while
the Agent is working, then use the runtime's own input queue or before-model hook.

Choose **Steer** to handle new input at the next supported model boundary, or
**Queue** to wait until current work finishes. Neither mode interrupts a running
tool or cancels work. Answers and approval results resume their original wait in
both modes. A new ordinary message never silently becomes an approval.

## Setup

1. In the workspace, choose **Connect an Agent**, then your existing environment.
2. Save the supplied worker token in a private file or secret manager. Copy the
   setup instruction into your coding assistant, or use the matching binding below.
3. Send the connection check. Its reply must come from this exact worker instance.

The SDKs are currently distributed from the source checkout. Build TypeScript
with `npm --prefix sdk/typescript ci` and `npm --prefix sdk/typescript run build`;
install Python with `python3 -m pip install ./sdk/python`. Registry publication is
separate. There is no public `npx` install claimed here.

The source-checkout instruction generator reads no credential:

```sh
node sdk/typescript/bin/setup.mjs ai-sdk \
  --url https://your-workspace.example --instance ins_your_connection --mode steer
```

Use the existing worker credential. Do not enroll another Agent. Set
`NOTIF_URL`, `NOTIF_TOKEN_FILE`, `BEEPGRID_CONNECTION_ID` (the exact instance),
`BEEPGRID_INBOX_PATH` (a private persistent volume) and `BEEPGRID_BUSY_MODE`.
See [the TypeScript host helper](../examples/runtime/typescript.mjs) and
[the LangChain helper](../examples/runtime/langchain.py).

## Binding coverage

These are integration functions for your existing host, not replacement harnesses
or managed hosting. Native clients and optional framework packages remain yours.

| Environment | Input path | Busy modes | Host work |
| --- | --- | --- | --- |
| Codex app server | `codexBinding` | Steer, Queue | Existing RPC client, thread mapping and turn events |
| Pi RPC | `piBinding` | Steer, Queue | Existing RPC process and session mapping |
| AI SDK 7 | `aiSDKBinding` | Steer, Queue | Per-call steering object, checkpointed session and wake handler |
| LangChain / LangGraph | `LangGraphBridge` | Steer, Queue | Add middleware and use the graph's durable checkpointer |
| Claude Code, Goose, Gemini CLI | `acpBinding` | Queue; Steer only with negotiated extension | Existing ACP connection and session mapping |
| OpenCode | `openCodeBinding` | Queue | Existing server transport and idle notification |
| Claude Agent SDK | `claudeSDKBinding` | Queue | Existing streaming-input iterator, gated at turn completion |
| Custom TypeScript | `checkpointBinding` | Steer, Queue | Durable append, existing wake loop and result resolver |
| Custom Python | `LiveInbox` | Host-defined Steer, Queue | Local pending-input hook and durable checkpoint |

Wire contracts are tested for the native bindings. AI SDK and LangChain use real
installed frameworks with controlled, network-free models. Do not interpret these
checks as qualification of every harness version, provider or extension. OpenClaw
channel plugins, hosted MCP Events, Google ADK, PydanticAI and OpenAI Agents-native
bindings remain separate work; their applications can use the custom checkpoint
port today. A tools integration does not establish active steering.

## Codex

Use an existing initialized app-server client. Map each Beepgrid conversation to
its existing native thread, persist that mapping, and track active turn IDs from
native events. This does not attach to an arbitrary standalone terminal session.

```ts
const binding = codexBinding({
  rpc: appServer.request.bind(appServer),
  route: sessions.threadForConversation,
  activeTurn: sessions.activeTurn,
  idle: sessions.waitUntilIdle,
  reconcile: sessions.findDeliveryInTranscript,
});
```

Steer uses `turn/steer` with `expectedTurnId`. Idle input uses `turn/start`. Queue
waits for idle. A raced turn transition is reconciled, not cancelled and restarted.
The stable delivery ID is also present in message content and client metadata.
Keep native approvals, sandbox policies and tool authority in the host.

## Pi

```ts
const binding = piBinding({
  rpc: sessions.request,
  route: sessions.forConversation,
  reconcile: sessions.findDeliveryInTranscript,
});
```

RPC `prompt` includes `streamingBehavior: steer` or `followUp` and a stable command
ID. Acceptance is separate from subsequent streamed Agent events. Keep reading
those events in the existing host and checkpoint the native session normally.

## AI SDK

Create `experimental_createSteering()` once per `generateText`, `streamText` or
`ToolLoopAgent` call and pass it as `experimental_steering`.

```ts
const binding = aiSDKBinding({
  route: sessions.forConversation,
  steering: sessions.currentSteering,
  wake: sessions.appendAndWake,
  queue: sessions.appendForNextRun,
  reconcile: sessions.findDeliveryInCheckpoint,
});
```

The binding uses the stable delivery ID with `steer()`. If the run has already
finished or the input was not delivered, `wake` must append it durably and schedule
the existing session loop, with one writer. Persist each step's
`experimental_steeringMessages` as well as response messages. A `prepareStep`
messages override must preserve those messages. Queue appends for the next run.
The experimental API is version-sensitive; its checked consumer is AI SDK 7.0.159.

## LangChain

```python
inbox = LiveInbox(client, private_path, exact_instance, mode="steer")
bridge = LangGraphBridge(inbox)
agent = create_agent(model, tools=tools,
                     middleware=[*middleware, bridge.before_model()],
                     checkpointer=durable_checkpointer)
await bridge.serve(agent, on_result=publish)
```

`publish(conversation, result, key)` sends the output using the supplied stable
idempotency key. It must finish durably before returning. The supervisor runs one
invocation per conversation, inserts live input in local before-model middleware,
checks checkpointed IDs and handles arrivals at the completion boundary. No
Beepgrid HTTP call is made from the middleware. Queue takes the batch present at
invocation start. Different conversations can run independently, up to eight.

Reuse the application's existing thread ID mapping if it differs from the
Beepgrid conversation ID. The current helper deliberately uses the conversation
ID as `configurable.thread_id`; use a dedicated graph namespace if existing
threads use another scheme. Native LangGraph interrupts need the host's ordinary
resume path. The bridge stops visibly on a paused graph instead of overriding it.
Use a persistent checkpointer in deployment; in-memory test checkpoints are not
restart durability. Always close the inbox in `finally` during shutdown.

## ACP

Use the existing ACP client for Claude Code, Goose or Gemini CLI. `acpBinding`
uses `session/prompt` only when idle in Queue mode. It does not invent a standard
steering method. Supply `steer(session, text, id)` only after negotiating and
qualifying a native extension. Otherwise selecting Steer fails explicitly.

## OpenCode

`openCodeBinding` uses your authenticated request transport and mapped session.
Wait for the native idle event, then call `/session/:id/prompt_async`. Queue only
is advertised until active injection has been qualified. Do not abort a busy
session to simulate steering. Preserve existing server authentication and keep
local server endpoints private.

## Claude Agent SDK

`claudeSDKBinding` appends SDK user messages to your existing streaming iterator.
The application provides a durable `append(session, message, id, mode)` function,
gated by the native turn-complete event. It must wake an idle iterator and preserve
session IDs. This path advertises Queue, not interruption of an active long turn.
Claude Code's Channels preview is a different integration and is not enabled here.

## Custom code

TypeScript imports are available from `@notif/client/runtime`,
`@notif/client/runtime/node` and `@notif/client/runtime/bindings` in a local packed
consumer. Source-checkout examples use relative imports.

```ts
const binding = checkpointBinding({
  route: sessions.forConversation,
  append: sessions.appendInput,       // durable, deduplicated by input.id
  wake: sessions.wake,               // existing single-writer loop
  resolve: sessions.resumeRequest,   // correlated result, not ordinary queued work
  reconcile: sessions.findDeliveryInCheckpoint,
});
const store = new SQLiteInbox(privatePath, workspaceAndExactInstance);
const live = new LiveConnection(client, store, binding, "steer", reportError).start();
// On shutdown: live.stop(); await live.done(); store.close();
```

`submit` returns after native acceptance or host checkpointing, not after the
whole business task. `reconcile` returns `accepted`, `retry` only after proving
absence, or `unknown`. Unknown input stays visible in `store.pending()` as
`submitting`; it is not automatically replayed. Alert on this state. After operator
reconciliation, `setState(id, "pending")` explicitly permits another attempt.

Python `LiveInbox.start()` runs independent receiving. At the next allowed model
boundary, read `pending(conversation)`, preserve IDs and input metadata, persist
that input in the host's state, then `checkpoint(ids)`. Queue reads ordinary input
only at idle; request results resume the original waiter independently. Implement
the same idle/final-boundary wake mechanism as your existing host.

Use a durable remote implementation of `InboxStore` or a host checkpoint service
for ephemeral/serverless instances. Local SQLite needs a persistent private volume.
Pending custody is bounded at 1,000 entries; a full inbox pauses ACK rather than
dropping input. Dedup records are retained longer than the engine's 30-day delivery
lifetime. Custody is not a permanent workspace archive.

## MCP tools

Keep the normal MCP server for sending, asking, files and progress. When a live
receiver owns intake, configure `args = ["mcp", "--live-input"]`. Both discovery
and direct calls disable receive/acknowledge. Without a receiver, the original
MCP receive path remains supported and unchanged.

## Workers

The existing Go, Python and TypeScript sequential worker examples remain useful
for independent jobs. Their handlers run to completion. They are not described
as native steering integrations. See [worker setup](CONNECT-AGENT-RECIPES.md).

## Reliability and qualification

Test busy input, Queue behavior, idle wake, final-boundary arrival, result resume,
attachments, duplicate delivery, a lost native response, restart, revoked access,
backpressure and shutdown. Use a real PostgreSQL engine for authorization and
transport tests. A receipt means durable runtime custody, not model reading.
Peer content retains its sender and never gains the user's tool authority.
Running tools can delay the next model boundary. No fixed response-time SLA is
implied by a successful native submit.

Controlled framework checks are `scripts/runtime-ai-sdk-check.mjs` and
`scripts/runtime-langgraph-check.py`. They use real framework packages and local
model doubles without API spend. Engine integration checks include the receiver
in `scripts/sdk-integration.py`; journal recovery, fencing and wire contracts
also run in the ordinary SDK unit suites.
