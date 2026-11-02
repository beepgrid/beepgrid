# A shared conversation across runtimes

Builder opens a shared conversation with Reviewer and a person. Reviewer identifies two remaining jobs. Builder brings DevOps in, explicitly shares the earlier history and addresses it with a structured mention. DevOps replies, and the person supplies the next instruction.

Builder uses the Go SDK. Reviewer, DevOps and the person use the Python SDK. All messages and membership changes go through the real engine with separate credentials. The text is deterministic fixture data: no model runs, no deployment happens and the final instruction is an ordinary message rather than a typed approval.

## Run

Start `python3 scripts/playground.py` in one terminal. In another terminal, from the same checkout:

```sh
PYTHONPATH=sdk/python python3 examples/shared-conversation/main.py
```

The example uses `.local/owner-token` and localhost:4010 by default. For another disposable local engine, set `BEEPGRID_URL` and `BEEPGRID_TOKEN_FILE`. It requires an owner-only credential file and refuses non-loopback hosts. It creates three fixture agents and explicit contact grants, so use a disposable workspace.

Expected output:

```text
PASS: Go Builder, Python Reviewer/DevOps, explicit invitation, shared history and person reply.
Conversation: <conversation ID>
```

Open **Messages** in the workspace and select **Search API migration**. The assertions verify the five canonical messages, all four senders, the mention and DevOps's access to the explicitly shared history. Re-running creates a separate example conversation. Stop the playground with Ctrl+C, then `docker compose stop` to stop its database and mail containers while keeping local history.

For a typed approval with follow-ups or separate private fanout, see the [chat workflow recipes](../README.md#chat-workflows).
