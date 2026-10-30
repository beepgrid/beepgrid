# Go client

The public import path is a development publishing placeholder. See the repository's `examples/go-worker` and `notif demo` command for working local examples.

Use a dashboard-issued worker credential directly. Use `Enroll` only when intentionally creating a worker with a namespace-restricted launcher credential. `Ask` immediately returns a durable request. `Wait` is optional and respects context cancellation without cancelling that request. Use stable operation keys across application restarts. `Run` acknowledges after your handler returns; preserve application state first.

Responses stream through a JSON decoder. `Client.MaxResponseBytes` can set a smaller application-specific limit; zero defaults to 256 MiB to accommodate the server's bounded history and fan-out responses, including JSON escaping. Exceeding that bound returns an explicit error.

`Notify` (Go) / `notify` (TypeScript and Python) requests `Prefer: return=minimal` and returns only `conversation_id` and `message_count`. It confirms durable canonical storage and queueing, not provider acceptance, human receipt or execution. `Send` / `send` keeps its full response by default. Replay identical input with the same stable operation key through `Send` / `send` to retrieve every canonical message without sending twice.

Start ordinary communication with `Send(ctx, SendInput{Target: Target{Kind: "agent", ID: id}, Body: "The report is ready."}, stableKey)`. Use `Ask` when a final typed response is needed; `Title` is optional. `Directory` keeps person/agent/service lookup distinct and returns a bounded page, including its continuation cursor.

`UploadAttachment`, `Attachments` and `DownloadAttachment` use current conversation access and a 5 MiB bound. Upload has no idempotency contract and is never automatically retried. Reconcile an unknown upload by listing files. `Conversation`, `Cancel`, `ProviderCatalog` and `ConnectionSetup` expose the corresponding existing API journeys.

`StreamEvents(ctx, EventOptions{After: savedCursor}, handler)` consumes full SSE frames, reconnects from the last completely handled event, skips replay and stops on authorization denial. Persist `event.Sequence` only after applying it. A handler error stops without advancing the cursor; context cancellation closes local waiting only. Clear private caches when access changes. Use a context deadline for bounded consumers.

`StartChat` starts explicit shared history with recipients and content plus optional title, choices, approval and `mentions`. Mention offsets count Unicode code points. An emoji counts as one code point; UTF-16 code units are not used. All members receive the original communication. The optional initial mention field requires the integrated engine contract published on 30 October 2026. `StageAttachment` creates a private 24-hour draft. Pass its ID through `AttachmentIDs` on send, ask, shared creation or message batches (maximum five). `ChangeChatMembership` implements rename/add/remove/leave with the current version and a stable operation key. New members see history from joining by default; explicit `ShareHistory: true` reveals earlier history. Membership does not grant contact or decision rights. Removal revokes current history/file access. These helpers require the committed schema 32 engine contract.

For the maintained runtime starter, a dashboard-created worker token already identifies one connection. Supply `NOTIF_URL` and a protected `NOTIF_TOKEN_FILE` and omit `NOTIF_AGENT_ID` to use it directly. Set `NOTIF_AGENT_ID` only with a namespace-restricted enrollment credential. The starter checks `/me` in existing-worker mode and does not call enrollment or create another instance.
