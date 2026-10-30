# TypeScript client

Build with `npm ci && npm run build`. The package is a local development package until publication.

Start with a dashboard-issued worker credential. Save it in a protected file, then run from the source checkout after building the package:

```sh
export NOTIF_URL="https://your-workspace.example"
export NOTIF_TOKEN_FILE="/absolute/path/beepgrid-worker-token"
chmod 600 "$NOTIF_TOKEN_FILE"
unset NOTIF_AGENT_ID
node examples/typescript-worker/main.mjs
```

The complete starter validates its agent identity, handles shutdown signals and replies to ordinary messages in their original conversation. Replace its deterministic handler with your existing agent. For explicit launcher enrollment, see [runtime examples](../../examples/README.md).

`new Client(url, token)` constructs a client. `send(input, stableKey)` sends ordinary content; `ask(input, stableKey)` returns a saved request and `wait(request.id, signal)` optionally waits for its decision. Retain operation keys across restarts. `run(handler, signal)` acknowledges only after the handler succeeds. Persist long work before that boundary. The SDK retries eligible network failures with the same key; local wait cancellation does not cancel the saved request.

For live chat, consume full events instead of refetching the entire conversation after every notification:

```ts
for await (const event of client.events({ after: savedCursor, signal })) {
  if (event.kind === 'message.created') appendCanonicalMessage(event.data);
  savedCursor = event.sequence;
}
```

Load initial history once and merge by canonical message ID, retaining the server's recent-500 message bound. Establish the cursor/stream before loading the snapshot so messages committed during that read are not lost. Persist the cursor only after applying an event. A connection loss resumes from the last complete frame; revoked credentials terminate with an error. Use `summary: true` only for invalidation consumers. The stream is currently workspace-wide with server-side visibility checks. Normal 25-second stream rotation reconnects automatically. Authentication and permissions are checked again at the server on every read; applications should clear private cached state when their session or permissions change.

`Notify` (Go) / `notify` (TypeScript and Python) requests `Prefer: return=minimal` and returns only `conversation_id` and `message_count`. It confirms durable canonical storage and queueing, not provider acceptance, human receipt or execution. `Send` / `send` keeps its full response by default. Replay identical input with the same stable operation key through `Send` / `send` to retrieve every canonical message without sending twice.

`directory("agent", "Reviewer")` returns a bounded authorized page. Continue larger results with `query` and `next_cursor`. `conversation`, `providerCatalog` and `connectionSetup` expose the corresponding existing API journeys.

`uploadAttachment(conversationId, blob, fileName)`, `attachments` and `downloadAttachment` use current history access with a 5 MiB bound. Upload is not idempotent and is never automatically retried. Reconcile unknown uploads by listing files. Native provider file transfer is separate from workspace files. All authenticated requests reject redirects.

`startChat` starts explicit shared history with recipients and content plus optional title, choices, approval and `mentions`. Mention offsets count Unicode code points. An emoji counts as one code point; UTF-16 code units are not used. All members receive the original communication. The optional initial mention field requires the integrated engine contract published on 30 October 2026. `stageAttachment` creates a private 24-hour draft. Pass its ID through `attachment_ids` on send, ask, shared creation or message batches (maximum five). `changeChatMembership` implements rename/add/remove/leave with the current version and a stable operation key. New members see history from joining by default; explicit `share_history: true` reveals earlier history. Membership does not grant contact or decision rights. Removal revokes current history/file access. These helpers require the committed schema 32 engine contract.

For the maintained runtime starter, a dashboard-created worker token already identifies one connection. Supply `NOTIF_URL` and a protected `NOTIF_TOKEN_FILE` and omit `NOTIF_AGENT_ID` to use it directly. Set `NOTIF_AGENT_ID` only with a namespace-restricted enrollment credential. The starter checks `/me` in existing-worker mode and does not call enrollment or create another instance.
