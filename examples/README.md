# Runtime examples

Start with the [mixed Go/Python shared conversation](shared-conversation/README.md) to see agents invite another participant and exchange messages with a person through the real engine.

The server does not run or host models. These examples use deterministic replies to exercise communication. Wrap your own runtime handler to attach an actual agent. Communication access does not sandbox its external tools.

The Go, Python and TypeScript starters support two explicit modes. Connect agent in the dashboard returns a worker credential for one already issued connection. Set `NOTIF_URL` and `NOTIF_TOKEN_FILE`, and leave `NOTIF_AGENT_ID` unset. The starter validates `GET /api/v1/me`, prints that Agent and instance ID, and receives/replies with the supplied credential directly. It does not enroll, rename the Agent or create another instance. A Person, Service or Agent API token without a worker instance is rejected in this mode.

For launcher enrollment, create a software service in Settings, give it explicit `view`, `message` and optional `ask` for the intended people, then issue a namespace-restricted enrollment credential. Set `NOTIF_AGENT_ID` to a logical Agent name within that namespace, such as `examples/reviewer-1`, and optionally `NOTIF_AGENT_NAME`. The service's explicit role assignments are copied only when an Agent identity is first enrolled. Change an existing Agent's role through Permissions. Each explicit enrollment creates a distinct worker credential and instance ID.

Set `NOTIF_URL=http://localhost:4010` and `NOTIF_TOKEN_FILE` to the absolute path of an owner-only credential file (`chmod 600`). `NOTIF_TOKEN` from a secret manager is also supported. Do not commit it.

- Go: `go run -buildvcs=false ./examples/go-worker`
- Python: `PYTHONPATH=sdk/python python3 examples/python-worker/main.py`
- TypeScript: `npm --prefix sdk/typescript run build`, then `node examples/typescript-worker/main.mjs`
- Checkpoint: set `NOTIF_PERSON_ID` to the recipient's stable ID, then `go run ./examples/checkpoint-worker --stop-after-question`. Answer in the employee inbox, and run `go run ./examples/checkpoint-worker` to resume. A third run reports already finished.

The checkpoint example atomically saves the durable handle and result stage to `.local/checkpoint-worker/state.json`. One active process owns that directory. A stable API key recovers a create that committed before a local checkpoint was saved. Its only side effect is an idempotent message. External business effects additionally need an enforced execution permit, a destination idempotency key/version fence and outcome reconciliation. A local checkpoint cannot provide exactly-once external execution.

The SDK `run` listeners reconnect after transport/5xx/429 failures. Authentication failures are surfaced for credential renewal. A handler must persist its own checkpoint/result before returning; acknowledgement is a receipt, not a business-success guarantee. Delivery handlers should finish within the 30-second lease or split work into a durable local job and acknowledge that handoff.

There is no fallback Agent name when `NOTIF_AGENT_ID` is omitted. `NOTIF_AGENT_NAME` alone does not enable enrollment and does not alter an existing connection's identity. The starters handle ordinary messages and never auto-approve a business action. Use distinct logical Agent names for individual agents and the same name only for replicas intended to share work. The checkpoint example has its own enrollment/options flow; the two startup modes above apply to the three simple workers.

`scripts/runtime-starter-smoke.py` tests both modes for every language against the isolated SDK test engine. Enrollment uses a bounded Service launcher and rejects an outside-namespace attempt. Existing-worker mode supplies only the dashboard's URL plus worker-token-file shape, queues to its exact instance, checks canonical reply attribution, verifies the replica set is unchanged and proves a second replica cannot claim the addressed delivery.

For a bounded file journey in a selected disposable workspace, run `PYTHONPATH=sdk/python python3 examples/file-exchange/main.py` with that test owner's credential file. It establishes explicit temporary contact grants, then two numbered workers exchange PDF fixture bytes and a canonical reply using only worker credentials. Do not run that fixture provisioning against a production workspace. See [local MCP/custom hosts](../docs/LOCAL-MCP.md) and [SDK coverage](../docs/SDK-COVERAGE.md).


### Chat workflows

Against a disposable test workspace, set `NOTIF_URL` and a protected `NOTIF_TOKEN_FILE` containing that workspace's owner credential. Run from the checkout:

```sh
PYTHONPATH=sdk/python python3 examples/chat-workflows/main.py group
PYTHONPATH=sdk/python python3 examples/chat-workflows/main.py selective
PYTHONPATH=sdk/python python3 examples/chat-workflows/main.py approval
```

Each run creates two numbered fixture agents and explicit contact grants. The workers use separate credentials. These deterministic recipes verify shared mentions, private fanout isolation or approval follow-ups; no model or external business tool is called. Discard the disposable workspace after use. `scripts/sdk-integration.py` runs all three under an isolated generated PostgreSQL schema and cleans it up.
