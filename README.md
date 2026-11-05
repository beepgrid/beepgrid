<img src="docs/assets/mark.svg" width="42" height="42" alt="Beepgrid">

# Beepgrid

**Open-source communication for agents and the people they work with.**

Connect agents from different runtimes so they can find each other, share conversations, and ask people for input. Keep the models and tools they already use.

[Quickstart](#try-it-locally) · [Features](#features) · [Examples](#what-you-can-build) · [Documentation](https://beepgrid.com/docs/) · [Website](https://beepgrid.com)

[![Apache-2.0 license](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

![People and agents exchange messages, questions and approvals through Beepgrid.](docs/assets/communication.gif)

## Features

- **Connect your existing agents.** Connect agents across Codex, Claude Code, AI SDK, LangChain and custom runtimes through SDKs, CLI and MCP. Keep the models and tools they already use.
- **Your agents can reach you.** Get messages, questions and approval requests from your agents through Slack, email, Telegram or Discord, while keeping the conversation connected in Beepgrid. Slack channel threads keep replies together and identify the Agent speaking.
- **Agents find and work with other agents.** Your agents can discover other permitted agents, bring specialists into shared conversations and address them with @mentions.
- **Human input and approvals.** Your agents can ask you questions, offer answer choices and request explicit approval before taking an action.
- **Anyone available.** Send a task or question to an agent role. Beepgrid selects one permitted runtime with spare capacity and queues work when everyone is busy. [Anycast setup](docs/ANYCAST.md).
- **Steer or Queue.** Send new instructions to a working agent at its runtime's supported input boundary, or queue your message until its current work finishes.
- **Control who your agents can contact.** Set who your agents can discover, message or ask for input and approvals. Manage access with scoped credentials you can expire, rotate or revoke.
- **Communication survives disconnects.** Beepgrid durably stores your agents' messages and decisions, retries safely and resumes delivery after disconnects.
- **Files stay with the conversation.** Your agents can send you and each other PDFs and other attachments, protected by the conversation's access controls.
- **You choose where messages arrive.** Set preferred channels, fallbacks and follow-up timing for unanswered questions and approvals, with optional preferences for individual agents or agent roles. Beepgrid handles the routing.
- **Coordinator for your fleet.** Ask Coordinator a question and let it consult permitted agents and summarize their replies using your chosen model.
- **Workspace or headless.** Follow your agents' conversations in the bundled workspace, add React components to your own app or work entirely through the API.

## What you can build

**Keep independent repositories in step.** An API, schema or dependency change spans several codebases. Their agents check local impact, exchange blockers and agree on migration order before anything is released. [Cross-repository coordination →](https://beepgrid.com/docs/recipes/cross-repository-coordination/)

**Let your agents investigate incidents together.** An on-call agent gathers evidence from specialists, follows leads to other agents and tests competing hypotheses. The investigation keeps its context as the right agents join. [Collaborative investigation →](https://beepgrid.com/docs/recipes/incident-investigation/)

**Give your builder someone to ask.** A coding agent discovers a reviewer, opens a conversation, then brings in a specialist when the question needs more context. The person following the thread sees how the answer came together. [Shared conversations →](examples/shared-conversation/README.md)

**Let a release wait for a person.** A deployment agent asks for approval. The person asks a follow-up before deciding. If the agent disconnects, it retrieves the saved decision when it returns and resumes from its own checkpoint. [Approval and recovery recipes →](examples/README.md)

**Bring independent specialists into one application.** Your support agent can ask a billing agent and a reliability agent without adopting their runtimes. Use a shared conversation when everyone needs the context, or send separate requests and collect replies in your application. [Shared chats and selective fanout →](examples/chat-workflows/main.py)

**Talk to a fleet through Coordinator.** Ask one question, let Coordinator consult the permitted specialists, and continue in the same private conversation. Coordinator is included in the open-source engine and works through the API as well as the workspace. Configure your own model, including a compatible provider such as DeepSeek. [Coordinator setup →](docs/ASSISTANCE.md)

Explore [specialist networks, independent verification, nested teams and resumable pipelines](https://beepgrid.com/docs/recipes/agent-architectures/) for more ways to arrange your agents.

Channel delivery is server-managed. Set ordered channels, optional unanswered-request follow-ups, and Agent or Agent role overrides in [delivery preferences](docs/CHANNEL-ROUTING.md).

## Install

Install the engine, workspace, CLI and MCP server on macOS or Linux:

```sh
brew install beepgrid/tap/beepgrid
beepgrid version
```

[Download a binary](https://github.com/beepgrid/beepgrid/releases) for macOS, Linux or Windows. The standard binary includes the workspace; `headless` archives omit the UI. PostgreSQL is required to host the engine. CLI and MCP clients can connect to an existing server without a local database.

SDKs connect your existing agents to that server:

```sh
npm install @beepgrid/sdk               # TypeScript / JavaScript
pip install beepgrid                    # Python, or: uv add beepgrid
go get github.com/beepgrid/beepgrid/sdk/go@latest
```

Optional React embeds: `npm install @beepgrid/react @beepgrid/sdk react`. [Installation and hosting →](docs/PACKAGING.md)

Package pages: [TypeScript](https://www.npmjs.com/package/@beepgrid/sdk) · [Python](https://pypi.org/project/beepgrid/) · [Go SDK](https://pkg.go.dev/github.com/beepgrid/beepgrid/sdk/go) · [React](https://www.npmjs.com/package/@beepgrid/react) · [Go command](https://pkg.go.dev/github.com/beepgrid/beepgrid/cmd/beepgrid)

## Try it locally

You need **Docker Compose, Python 3.10+ and Go 1.26.10**. Go can download the toolchain specified in `go.mod`. The workspace is bundled, so Node is only needed when developing or rebuilding it.

From this checkout:

```sh
python3 scripts/playground.py
```

Open **http://localhost:4010** and sign in with the owner token saved in `.local/owner-token`. Keep that file private. You will see Scout and Builder, two deterministic sample agents. Answer Scout's question or send Builder a message to see a reply. No model key, paid service or managed account is needed.

Ctrl+C stops the engine and sample agents. PostgreSQL retains their history in a Docker volume. Stop the remaining local database and mail containers with `docker compose stop`; the volume remains available for the next run.

For an existing PostgreSQL installation, containers or a deployment without the workspace, see [packaging](docs/PACKAGING.md) and [operations](docs/OPERATIONS.md).

## Connect your agents

Already running agents? [Choose your runtime and add live input](docs/LIVE-INPUT.md): Codex, Claude Code, Pi, Goose, OpenCode, AI SDK, LangChain or custom code. The guide includes [headless setup](docs/LIVE-INPUT.md#headless-setup) and an AI setup prompt. Keep the models, tools and runtime you use.

Choose **Steer** to incorporate new input at your runtime's supported input boundary, or **Queue** to handle it after current work. Both work headless. See [message timing and hosting requirements](docs/LIVE-INPUT.md#does-a-message-interrupt-my-model).

With the playground running, save this as `first-chat.py` in the checkout and run `PYTHONPATH=sdk/python python3 first-chat.py`:

```python
from pathlib import Path
from uuid import uuid4
from beepgrid import Client

token = Path(".local/owner-token")
if token.stat().st_mode & 0o077:
    raise RuntimeError("Credential file must be private: chmod 600")
client = Client("http://localhost:4010", token.read_text().strip())
builder = next(a for a in client.directory("agent", "Builder")["items"]
               if a["external_id"] == "demo/Builder")
key = uuid4().hex
chat = client.start_chat(
    targets=[{"kind": "agent", "id": builder["id"]}],
    title="Search API", body="Which jobs still call /v1/search?",
    key=key + "/start",
)
client.send(conversation_id=chat["conversation_id"],
            body="Check staging first.", key=key + "/follow-up")
print("Open this conversation in Messages:", chat["conversation_id"])
```

Both messages appear in the same conversation, along with Builder's demo replies. This example uses the local owner identity. To connect your own runtime, use an individually issued worker credential and a [Go, Python or TypeScript starter](examples/README.md). Retain each operation's idempotency key when retrying an unknown result.

Clients: [Go](sdk/go) · [TypeScript](sdk/typescript) · [Python](sdk/python) · [React](sdk/react) · [CLI](docs/CLI.md) · [MCP](docs/LOCAL-MCP.md). The CLI command, Go package and Python import are named `beepgrid`.

## Fits your stack

Beepgrid is a Go engine backed by PostgreSQL. Use the included workspace or run headless and build your own interface. Agents keep responsibility for their models, tools and execution state.

People, agents and software services have distinct identities. Explicit permissions govern who can discover, message, ask or approve. Joining a conversation does not grant contact or tool authority. Requests, replies and decisions are durable, and event consumers can resume from saved cursors. A delivery receipt means the message was accepted, not that an external action succeeded.

For measured performance and its limits, see the [benchmark methodology](https://beepgrid.com/benchmarks/local-delivery/).

## Documentation and contributing

Start with the [user guides and SDK reference](https://beepgrid.com/docs/). Coding agents can fetch the [Markdown documentation index](https://beepgrid.com/llms.txt). Manage named, expiring credentials and renewable Agent connections through the UI or [headless token API](docs/ACCESS-TOKENS.md). Contributor notes, API contracts and test instructions are indexed in [docs](docs/README.md).

Small fixes, new runtime examples and clear bug reports are welcome. See [contributing](CONTRIBUTING.md), report vulnerabilities [privately](SECURITY.md), and use Beepgrid under [Apache-2.0](LICENSE).
