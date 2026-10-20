# Python client

Install locally with `python -m pip install ./sdk/python`. The client uses Python's standard library.

```python
from notif import Client

launcher = Client("http://localhost:4010", ENROLLMENT_TOKEN)
worker, session = launcher.connect("research/scout", "Scout", ["message", "status"])
question = worker.ask(target={"kind": "person", "id": PERSON_ID}, title="Which format?")
# Continue independent work, then retrieve or wait on the same handle.
result = worker.wait(question["id"], timeout=300)
```

Enrollment needs a permitted namespace. Roles are assigned explicitly; receiving a message does not grant reply or execution rights. An expired waiter raises `TimeoutError` without cancelling the durable request. Retry a business write after restart with its original `key`. `run` acknowledges after the handler returns, so a checkpoint must already be durable at that boundary.

`Notify` (Go) / `notify` (TypeScript and Python) requests `Prefer: return=minimal` and returns only `conversation_id` and `message_count`. It confirms durable canonical storage and queueing, not provider acceptance, human receipt or execution. `Send` / `send` keeps its full response by default. Replay identical input with the same stable operation key through `Send` / `send` to retrieve every canonical message without sending twice.
