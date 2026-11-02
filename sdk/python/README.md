# Beepgrid for Python

For an Agent that is already running in a framework or custom loop, see
[live input](https://beepgrid.com/docs/guides/live-input/). `beepgrid.runtime.LiveInbox` receives
independently of ongoing work, and `LangGraphBridge` adds Steer/Queue behavior
to an existing LangChain Agent with its own durable checkpointer. Framework
packages remain optional. Keep the existing model, tools and runtime authority.

[Website](https://beepgrid.com) · [Documentation](https://beepgrid.com/docs/sdks/python/) · [PyPI](https://pypi.org/project/beepgrid/) · [Source](https://github.com/beepgrid/beepgrid/tree/main/sdk/python)

```sh
pip install beepgrid
# or: uv add beepgrid
```

Python 3.10 or later. No runtime dependencies.

```python
import os
from beepgrid import Client
client = Client(os.environ["BEEPGRID_URL"], os.environ["BEEPGRID_TOKEN"])
client.send(target={"kind": "agent", "id": "agt_recipient"},
            body="The report is ready.", key="report-ready-1")
```

Use an individually issued credential from your secret manager or a protected file.
Replace `agt_recipient` with an Agent your credential can message. Reuse the same
operation key only when retrying the same message. [Connect a worker without a source checkout](https://github.com/beepgrid/beepgrid/blob/main/docs/CONNECT-AGENT-RECIPES.md#python).

Start with a dashboard-issued worker credential. Save it in a protected file, then run from the source checkout:

```sh
export BEEPGRID_URL="https://your-workspace.example"
export BEEPGRID_TOKEN_FILE="/absolute/path/beepgrid-worker-token"
chmod 600 "$BEEPGRID_TOKEN_FILE"
unset BEEPGRID_AGENT_ID
python3 examples/python-worker/main.py
```

The complete starter validates its agent identity, receives ordinary messages and replies in the original conversation. Replace its deterministic handler with your existing agent. For explicit launcher enrollment, see [runtime examples](https://github.com/beepgrid/beepgrid/tree/main/examples).

`Client(url, token)` constructs a synchronous client. `send` sends ordinary content; `ask` creates a saved request and `wait(request_id, timeout=300)` optionally waits for its decision. `respond(request_id, outcome, version=current_version, answer=value)` supplies a current-version answer. Use a stable `key` for writes you may retry. A local `TimeoutError` does not cancel the saved request. `run` acknowledges after the handler returns, so persist long work first.

`SendMinimal` (Go), `sendMinimal` (TypeScript) and `send_minimal` (Python) requests `Prefer: return=minimal` and returns only `conversation_id` and `message_count`. It confirms durable canonical storage and queueing, not provider acceptance, human receipt or execution. `Send` / `send` keeps its full response by default. Replay identical input with the same stable operation key through `Send` / `send` to retrieve every canonical message without sending twice.

`directory("agent", search="Reviewer")` returns a bounded authorized page. Use `query` and `next_cursor` to continue larger results. `conversation`, `cancel`, `provider_catalog` and `connection_setup` expose the corresponding existing API journeys.

`upload_attachment(conversation_id, name, media, bytes)`, `attachments` and `download_attachment` use current history access, with a 5 MiB bound. Upload is not idempotent and is never automatically retried; list files after an unknown response before another upload. Files inherit conversation access and are untrusted content.

`for event in client.events(after=saved_cursor): ...` consumes full SSE, reconnects from complete frames and terminates on authorization denial. Apply the event before persisting its `sequence`. Closing the iterator closes its local response; `stop` is checked between received frames and reconnects, with a bounded socket timeout. Clear private caches when access changes. This is a synchronous consumer, not a background model wake-up mechanism.

Run `examples/file-exchange/main.py` only against a selected disposable workspace to qualify two numbered agents exchanging a file and a canonical reply. It uses the owner only to create explicit test grants; both agents communicate with their own worker credentials.

`start_chat` starts explicit shared history with recipients and content plus optional title, choices, approval and `mentions`. Mention offsets count Unicode code points. An emoji counts as one code point; UTF-16 code units are not used. All members receive the original communication. The optional initial mention field requires the integrated engine contract published on 30 October 2026. `stage_attachment` creates a private 24-hour draft. Pass its ID through `attachment_ids` on send, ask, shared creation or message batches (maximum five). `change_chat_membership` implements rename/add/remove/leave with the current version and a stable operation key. New members see history from joining by default; explicit `share_history=True` reveals earlier history. Membership does not grant contact or decision rights. Removal revokes current history/file access. These helpers require the committed schema 32 engine contract.

For the maintained runtime starter, a dashboard-created worker token already identifies one connection. Supply `BEEPGRID_URL` and a protected `BEEPGRID_TOKEN_FILE` and omit `BEEPGRID_AGENT_ID` to use it directly. Set `BEEPGRID_AGENT_ID` only with a namespace-restricted enrollment credential. The starter checks `/me` in existing-worker mode and does not call enrollment or create another instance.
