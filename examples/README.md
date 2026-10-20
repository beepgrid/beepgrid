# Runtime examples

The server does not run or host models. These examples use deterministic replies to exercise communication. Wrap your own runtime handler to attach an actual agent. Communication access does not sandbox its external tools.

Create a software service in Settings, give it a role with `view`, `message` and `ask` for the intended people, then issue an enrollment credential restricted to `examples/`. The service's explicit role assignments are copied only when an agent identity is first enrolled. Change an existing agent's role through Permissions. Each connection receives a distinct worker credential and instance ID.

Set `NOTIF_URL=http://localhost:4010` and `NOTIF_TOKEN` to that launcher credential in your shell or secret manager. Do not commit it.

- Go: `go run ./examples/go-worker`
- Python: `PYTHONPATH=sdk/python python3 examples/python-worker/main.py`
- TypeScript: `npm --prefix sdk/typescript run build`, then `node examples/typescript-worker/main.mjs`
- Checkpoint: set `NOTIF_PERSON_ID` to the recipient's stable ID, then `go run ./examples/checkpoint-worker --stop-after-question`. Answer in the employee inbox, and run `go run ./examples/checkpoint-worker` to resume. A third run reports already finished.

The checkpoint example atomically saves the durable handle and result stage to `.local/checkpoint-worker/state.json`. One active process owns that directory. A stable API key recovers a create that committed before a local checkpoint was saved. Its only side effect is an idempotent message. External business effects additionally need an enforced execution permit, a destination idempotency key/version fence and outcome reconciliation. A local checkpoint cannot provide exactly-once external execution.

The SDK `run` listeners reconnect after transport/5xx/429 failures. Authentication failures are surfaced for credential renewal. A handler must persist its own checkpoint/result before returning; acknowledgement is a receipt, not a business-success guarantee. Delivery handlers should finish within the 30-second lease or split work into a durable local job and acknowledge that handoff.
