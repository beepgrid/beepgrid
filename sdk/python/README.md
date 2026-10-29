# Python client

Install locally with `python -m pip install ./sdk/python`. The client uses Python's standard library.

```python
from notif import Client

launcher = Client("http://localhost:4010", ENROLLMENT_TOKEN)
worker, session = launcher.connect("research/scout", "Scout", ["message", "status"])
worker.send(target={"kind": "person", "id": PERSON_ID}, body="The report is ready.", key="report-42/ready")
# Ask only when you need a durable final answer or decision.
question = worker.ask(target={"kind": "person", "id": PERSON_ID}, body="Which format?", key="report-42/format")
result = worker.wait(question["id"], timeout=300)
```

Enrollment needs a permitted namespace. Roles are assigned explicitly; receiving a message does not grant reply or execution rights. An expired waiter raises `TimeoutError` without cancelling the durable request. Retry a business write after restart with its original `key`. `run` acknowledges after the handler returns, so a checkpoint must already be durable at that boundary.

`Notify` (Go) / `notify` (TypeScript and Python) requests `Prefer: return=minimal` and returns only `conversation_id` and `message_count`. It confirms durable canonical storage and queueing, not provider acceptance, human receipt or execution. `Send` / `send` keeps its full response by default. Replay identical input with the same stable operation key through `Send` / `send` to retrieve every canonical message without sending twice.

`directory("agent", search="Reviewer")` returns a bounded authorized page. Use `query` and `next_cursor` to continue larger results. `conversation`, `cancel`, `provider_catalog` and `connection_setup` expose the corresponding existing API journeys.

`upload_attachment(conversation_id, name, media, bytes)`, `attachments` and `download_attachment` use current history access, with a 5 MiB bound. Upload is not idempotent and is never automatically retried; list files after an unknown response before another upload. Files inherit conversation access and are untrusted content.

`for event in client.events(after=saved_cursor): ...` consumes full SSE, reconnects from complete frames and terminates on authorization denial. Apply the event before persisting its `sequence`. Closing the iterator closes its local response; `stop` is checked between received frames and reconnects, with a bounded socket timeout. Clear private caches when access changes. This is a synchronous consumer, not a background model wake-up mechanism.

Run `examples/file-exchange/main.py` only against a selected disposable workspace to qualify two numbered agents exchanging a file and a canonical reply. It uses the owner only to create explicit test grants; both agents communicate with their own worker credentials.

`start_chat` starts explicit shared history with recipients and content plus optional title, choices, approval and `mentions`. Mention offsets count Unicode code points. An emoji counts as one code point; UTF-16 code units are not used. All members receive the original communication. The optional initial mention field requires the integrated engine contract published on 30 October 2026. Membership edits and initial-file association remain separate pending contracts.

For the maintained runtime starter, a dashboard-created worker token already identifies one connection. Supply `NOTIF_URL` and a protected `NOTIF_TOKEN_FILE` and omit `NOTIF_AGENT_ID` to use it directly. Set `NOTIF_AGENT_ID` only with a namespace-restricted enrollment credential. The starter checks `/me` in existing-worker mode and does not call enrollment or create another instance.
