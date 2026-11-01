# Connect live input to your existing Agent

Keep your model, tools and runtime. Add a receiver that can accept input while
the Agent is working, then use the runtime's own input queue or before-model hook.

Choose **Steer** to handle new input at the next supported model boundary, or
**Queue** to wait until current work finishes. Beepgrid never sends cancellation or cancel/replay as a substitute for input.
Native steering follows the runtime's own boundary and tool behavior. The
recommended Claude SDK path waits for running tools; its ACP adapter may
pre-empt generation, so qualify busy-tool behavior for the installed version. Answers and approval results resume their original wait in
both modes through the host's result-resume handler. A new ordinary message never
silently becomes an approval.

## Set up with your AI

In the workspace, choose **Connect an Agent** and your existing environment.
Save the token, then copy **Instructions for your AI** into the coding assistant
working on your Agent's project. The prompt includes this guide, the exact
connection ID and environment-specific steps. It asks for the token file's path,
not the secret. Changing the environment does not issue another credential.

No workspace or browser is required for an open-source installation. See
[headless setup](#headless-setup), then choose your environment below. Both paths
use the same receiver, bindings, credentials and engine API.

## Manual setup

Run engine/SDK preparation in a Beepgrid checkout. Add the integration to the
project that **already runs your Agent**, not to a second replacement Agent.

1. Get the source with `git clone https://github.com/beepgrid/beepgrid.git` if
   needed. Record that checkout's absolute path.
2. Save the supplied connection token in a private file outside the checkout,
   or use your existing secret manager. For a terminal with Python 3, this asks
   for hidden input, creates a new mode-0600 file and refuses to overwrite one:

   ```sh
   python3 - <<'PYTOKEN'
   import getpass, os
   from pathlib import Path
   folder = Path.home() / ".config" / "beepgrid"
   folder.mkdir(parents=True, exist_ok=True, mode=0o700)
   path = folder / "worker-token"
   token = getpass.getpass("Paste connection token (hidden): ").strip()
   if not token:
       raise SystemExit("No token entered")
   fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
   with os.fdopen(fd, "w") as file:
       file.write(token)
   print("Saved token file:", path)
   PYTOKEN
   ```

   The secret is neither a command argument nor shell history. Keep the terminal
   private. Use a different filename for each connection. A workspace token
   expires after 24 hours; rotate deliberately and update the existing host's
   secret rather than enrolling on every message or process restart.
3. Prepare the SDK in the **Beepgrid checkout**:

   ```sh
   npm --prefix sdk/typescript ci
   npm --prefix sdk/typescript run build
   # For Python instead:
   python3 -m pip install ./sdk/python
   ```

   SDKs currently ship from source. There is no published `npx` installer claimed
   here. Link the local SDK into your Agent project, or import by absolute path
   as in the [TypeScript host helper](../examples/runtime/typescript.mjs).
4. Set these in the **existing Agent host's environment**. Replace the explicit
   example values with your engine origin, supplied instance ID and private paths:

   ```sh
   unset NOTIF_TOKEN
   export NOTIF_URL=https://your-engine.example
   export NOTIF_TOKEN_FILE="$HOME/.config/beepgrid/worker-token"
   export BEEPGRID_CONNECTION_ID=ins_your_connection
   export BEEPGRID_INBOX_PATH="$HOME/.local/share/beepgrid/connection/inbox.db"
   export BEEPGRID_BUSY_MODE=steer
   ```

   `NOTIF_TOKEN` otherwise overrides the file. The inbox needs a private persistent
   volume on a VM/container, or a durable remote `InboxStore` on an ephemeral host.
   Use `queue` for a Queue-only binding. An instance ID is an identifier, never a token.
5. Choose a binding in the table below. Reuse the native session and model loop;
   wire the existing host's routing, checkpoint and result callbacks. The snippets
   show those host-owned callbacks explicitly, not undefined magic auto-detection.
   Start the [receiver helper](../examples/runtime/typescript.mjs), or the
   [LangChain bridge](../examples/runtime/langchain.py), inside that host.
6. Verify an idle message, a busy message, an approval result, an attachment and
   restart recovery. Send the workspace check if using the UI. Replies must come
   from the exact connection. A transport ACK is not proof of model execution.

For AI-guided setup without the workspace, the following command only **prints an
instruction prompt**. Run it in the Beepgrid checkout after building the SDK, then
paste its output into your coding assistant in the Agent project. It neither
launches an Agent nor reads a credential:

```sh
node sdk/typescript/bin/setup.mjs ai-sdk \
  --url https://your-engine.example --instance ins_your_connection --mode steer
```

## Headless setup

Start the engine using [packaging](PACKAGING.md). Use a scoped launcher credential
as explained in [CLI authentication](CLI.md#authenticate-without-a-browser).
In the Beepgrid checkout, save enrollment input such as:

```json
{"agent":"research/scout","name":"Scout","capabilities":["message","request"]}
```

With `NOTIF_URL` set and `NOTIF_TOKEN_FILE` pointing to that launcher, run:

```sh
.local/notif enroll --input enroll.json --out /private/path/scout-token
```

This creates a protected token file and prints connection metadata including the
instance ID, not the token. Reuse that file and instance in manual step 4 or the
instruction generator. Enrollment is deliberate setup, not part of the Agent's
receive loop. Give the runtime its worker credential, never the owner/launcher
credential. Contact permissions and enrollment authority remain separate.

An AI starting from the README should inspect the project's runtime and version,
choose the matching row below, read that section plus manual setup, and use the
source SDK. Plain HTTP, SDK calls and MCP tools remain available without the live
receiver. Adding tools alone does not make a busy or idle model consume messages.

## Binding coverage

These are integration functions for your existing host, not replacement harnesses
or managed hosting. Native clients and optional framework packages remain yours.

| Environment | Input path | Busy modes | Host work |
| --- | --- | --- | --- |
| Codex app server | `codexBinding` | Steer, Queue | Existing RPC client, thread mapping and turn events |
| Pi RPC | `piBinding` | Steer, Queue | Existing RPC process and session mapping |
| AI SDK 7 | `aiSDKBinding` | Steer, Queue | Per-call steering object, checkpointed session and wake handler |
| LangChain / LangGraph | `LangGraphBridge` | Steer, Queue | Add middleware and use the graph's durable checkpointer |
| Claude Code / Codex via ACP | `acpBinding` | Steer, Queue with advertised extension | Existing initialized ACP client and session mapping |
| Goose ACP | `gooseBinding` | Steer, Queue on reviewed extension | Existing ACP client and session/update observer |
| Gemini CLI / other ACP | `acpBinding` | Queue; Steer with supported extension | Check capabilities, never assume ACP implies steering |
| OpenCode V1 | `openCodeBinding` | Queue | Existing server transport and idle notification |
| OpenCode V2 | `openCodeV2Binding` | Steer, Queue | Existing V2 client and durable inbox |
| Claude Agent SDK | `claudeSDKBinding` | Steer, Queue | Existing streaming-input iterator with native priorities |
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
const binding = withResultResume(codexBinding({
  rpc: appServer.request.bind(appServer),
  route: sessions.threadForConversation,
  activeTurn: sessions.activeTurn,
  idle: sessions.waitUntilIdle,
  reconcile: sessions.findDeliveryInTranscript,
}), requestWaiters.resume);
```

Steer uses `turn/steer` with `expectedTurnId`. Idle input uses `turn/start`. Queue
waits for idle. A raced turn transition is reconciled, not cancelled and restarted.
The stable delivery ID is also present in message content and client metadata.
Keep native approvals, sandbox policies and tool authority in the host.
`withResultResume(binding, resume)` works with every native binding. Its callback
resolves the original Beepgrid request waiter; it must not translate a result into
a new user approval or bypass the runtime's native approval flow. Without it,
Steer-capable bindings can receive a result as structured turn input, but cannot
promise to resume a host-blocked tool. Queue-only bindings leave such results
durable and report the missing handler instead of waiting for idle forever.

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

Pass `LangGraphBridge(inbox, thread_id=sessions.thread_for_conversation)` to
reuse an existing graph thread mapping. The default is the Beepgrid conversation
ID. Keep this mapping stable and isolate graph namespaces between applications. Native LangGraph interrupts need the host's ordinary
resume path. The bridge stops visibly on a paused graph instead of overriding it.
Use a persistent checkpointer in deployment; in-memory test checkpoints are not
restart durability. Always close the inbox in `finally` during shutdown.

## ACP

ACP is the common client-to-Agent protocol used by many coding harnesses. It
reduces integration work, but an entry in the [ACP ecosystem](https://zed.dev/acp)
is not a guarantee of a particular steering extension. Keep the existing ACP
client and its permission/file/terminal handlers, session mapping and event loop.

Codex ACP and Claude Agent ACP advertise the same extension:

```ts
const binding = acpBinding({
  initialization: existingInitializeResponse,
  rpc: (method, params) => acpClient.request(method, params),
  route: sessions.forConversation,
  idle: sessions.waitUntilIdle,
  reconcile: sessions.findDeliveryInTranscript,
});
```

This `rpc` port matches the generic request method in the current ACP TypeScript
SDK. Adapt it to your installed client's public prompt/extension methods if
using a different SDK version. Use the already completed initialization result;
do not initialize or launch a second session merely to add Beepgrid.

When advertised, `_session/steering` returns `injected` or `startedNewTurn`.
Both are input acceptance, not successful task completion. The default idle
behavior starts a turn, avoiding the ACP v1 trap where awaiting `session/prompt`
until turn completion prevents sending subsequent steering. Queue instead waits
for idle and uses ordinary `session/prompt`; it never calls the steering extension. Unknown outcomes/errors remain uncertain for reconciliation; no automatic
replay or cancellation follows. Continue consuming native completion/failure events.

Without the extension, `acpBinding` offers Queue through idle `session/prompt`.
An attempted Steer fails explicitly. A host can supply a documented `steer` callback
for another extension. [ACP v1](https://agentclientprotocol.com/protocol/v1/overview)
does not standardize non-cancelling steering. The
[Steer/Queue proposal](https://github.com/agentclientprotocol/agent-client-protocol/pull/1261)
remains a proposal at this review, not a universally available stable API.

The reviewed Gemini CLI ACP session aborts its pending prompt when another prompt
arrives. We therefore use Queue for that path. This is not a claim that all future
Gemini runtimes or custom Gemini SDK loops cannot support checkpoint input.

## Goose

Goose uses its own `_goose/unstable/session/steer` extension, fenced by
`expectedRunId`. Check the installed version against the
[reviewed schema](https://github.com/block/goose/blob/main/crates/goose/acp-schema.json).
It is not the Codex/Claude steering extension.

```ts
const binding = gooseBinding({
  rpc: acpClient.request.bind(acpClient),
  route: sessions.forConversation,
  idle: sessions.waitUntilIdle,
  reconcile: sessions.findDeliveryInTranscript,
  onError: reportNativeTurnError,
});
// Add to the existing session/update notification handler:
binding.observe(params);
```

Observe the existing session stream from connection start, including
`session_info_update._meta.goose.activeRunId`. For an idle prompt, the binding
waits for a matching `user_message_chunk` echo, output from the newly fenced run,
or native turn completion before accepting it. In the reviewed Goose source,
`start_reply` persists the incoming user message before returning the stream that
emits Agent text/thought/tool-call updates. Run-start metadata is emitted earlier
and alone is not custody. This requires one host writer for the mapped session.
These observations release the input lane early so later messages can steer. Any
subsequent turn failure goes to `onError`; keep the RPC/event consumer alive.
Busy input validates the returned run ID and message ID. A run transition or
missing/uncertain acceptance is reconciled, not cancelled or blindly retried.
Older versions without these events can use generic ACP Queue.

## OpenCode

**V2:** reuse the installed V2 client or plugin context:

```ts
const binding = openCodeV2Binding({
  prompt: (input) => client.session.prompt(input),
  route: sessions.forConversation,
  reconcile: sessions.findDeliveryInInboxOrTranscript,
});
```

Native `session.prompt` accepts `id`, `text`, metadata and
`delivery: "steer" | "queue"`. Its returned durable inbox entry is validated
against the deterministic ID, session and delivery mode. The native server
schedules execution. Do not wait for task completion to acknowledge admission.
See [V2 session API](https://dev.opencode.ai/v2/docs/build/plugins/server/#sessions).

**V1:** `openCodeBinding` waits for the native idle event and uses the existing
authenticated `/session/:id/prompt_async` transport. Queue only. Do not send a
V2 request to the V1 endpoint or claim that this older path has no possible future
steering integration. Keep local native server endpoints private.

## Claude Agent SDK

Use the native streaming-input iterator already passed to `query()`. No ACP
adapter is needed when your application already owns the SDK query:

```ts
const binding = claudeSDKBinding({
  route: sessions.forConversation,
  append: sessions.persistAndPushUserMessage,
  reconcile: sessions.findDeliveryInTranscript,
});
```

`append(session, message, id, mode)` takes durable, deduplicated custody and pushes
into that session's existing iterator, waking it when idle. The message uses
`priority: "next"` for Steer: the native runtime reads it after running tools
finish, within the same turn if possible. `priority: "later"` queues a new turn.
The SDK, not a second Beepgrid model loop, manages that boundary. Keep consuming
all SDK result and echo events; accepted input is not completed work.

`client_composed: true` keeps incoming text literal, without path expansion or
slash-command execution. This requires SDK 0.3.280 / CLI 2.1.248 or newer; the
published type consumer is pinned to SDK 0.3.316. Verify your installed version.
We do not set `priority: "now"`, which can interrupt work, or relabel another
Agent's message with human origin. Native permissions keep their existing handler.
See the [official input-priority semantics](https://code.claude.com/docs/en/agent-sdk/typescript#sdkusermessage).

The previous Queue-only guidance missed these current native priorities. That
was a gap in our adapter and research, not an inherent SDK limitation. Claude Code
hosts already using ACP can keep that interface; the two paths are alternatives,
not two receivers for the same worker instance.

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
dropping input. Accepted payloads are scrubbed immediately. Dedup IDs are retained longer than
the engine's 30-day delivery lifetime. Custody is not a permanent workspace archive.

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

The optional `BEEPGRID_CODEX_CHECK=1 node scripts/runtime-codex-check.mjs` uses an
existing signed-in Codex CLI, with no API-key fallback, in a read-only ephemeral
thread. It verifies a correction during active work and continuation to the end.
It is intentionally excluded from automatic verification because it uses a model.

## Interface choices and limits

There are seven runtime integration families: ACP; native harness APIs (Codex,
Pi, OpenCode); AI SDK; LangChain/LangGraph; Claude Agent SDK; custom checkpoint
hooks; and sequential language workers. MCP is an eighth, **tools-only** interface,
not another way to steer execution. All use the same Beepgrid message transport.
Python/TypeScript/Go worker examples are language conveniences for independent
message jobs, not separate communication protocols or requirements for live input.

Prefer native APIs when they expose clear acceptance, IDs and steering in an
existing host. Prefer ACP when that host already uses ACP, or to reuse its common
session/tool interface across compatible harnesses. ACP cannot seize an arbitrary
existing terminal session, and shared protocol support cannot manufacture a
missing runtime input boundary. Framework hooks keep the customer's existing loop
and require small integration callbacks; we deliberately do not secretly take
ownership of its model, persistence or native permission decisions.

The earlier Queue-only catalog was too conservative for Claude ACP, Goose and
OpenCode V2. This revision implements those paths. It also now maps Claude SDK's native priorities explicitly. It remains
conservative for reviewed Gemini ACP because a second concurrent prompt cancels
work there, rather than implementing non-cancelling steering. Python LangGraph's helper is tested with the
actual framework; JS LangGraph uses the custom checkpoint interface today.
