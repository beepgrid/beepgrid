# Beep Grid

The product wordmark is **beepgrid** and its domain is **beepgrid.com**. See [branding and interface inventory](docs/BRANDING.md). Existing `notif` package, configuration and command identifiers remain compatible while the public publishing namespace is finalized.

Connect your agents once, see them immediately, communicate with them individually or collectively, and control what each may do and who may contact whom.

Beep Grid is an Apache-2.0 Go engine with PostgreSQL and an embedded self-hosted web application. It connects your existing runtimes; it does not host models. People, agents and software services have separate APIs and management flows.

## Run the local playground

Requirements: Docker Compose, Go 1.26.10 or automatic Go toolchain download, Python 3.10+. Node 24 is needed only when developing/rebuilding the web packages. The committed web bundle makes a Go source build usable without a separate frontend server.

```sh
python3 scripts/playground.py
```

`make playground` is an equivalent shortcut where Make is installed.

Open [localhost:4010](http://localhost:4010). Copy the value from `.local/owner-token` into the local login form. On macOS, you can run `pbcopy < .local/owner-token` yourself. The token stays in a permission-restricted local file and becomes an HttpOnly browser session after login.

Scout asks a question in the inbox. Answer it, open its conversation, and message Scout or Builder from Agents. Both are deterministic examples with separate worker credentials. Ctrl+C stops the playground processes while preserving PostgreSQL history. Start it again to continue. Local mail inspection is available at [localhost:8125](http://localhost:8125).

To run the pieces separately:

```sh
docker compose up -d --wait
go build -buildvcs=false -o .local/notif ./cmd/notif
.local/notif serve --dev
# Other terminals, from this repository:
.local/notif demo --name Scout --ask
.local/notif demo --name Builder
```

Do not expose `--dev` on a public network. It permits local/private provider URLs and uses local bootstrap credentials. The server requires a loopback listening address in this mode.

## Connect your own agents

Create one software service under Settings, assign it a role for the intended people/groups, and issue an enrollment credential restricted to a namespace such as `research/`. This is launcher setup, not provisioning per process. A first SDK connection creates the agent identity automatically. Later connections create distinct instances under that stable identity. Names and metadata never grant permissions.

For a local source checkout, the CLI can create that restricted launcher in one step:

```sh
.local/notif seed --prefix research/ --out .local/research-launcher-token
```

It writes the token to a protected file and refuses to overwrite an existing one. The default role lets agents contact the owner; change the role in Permissions for your own audience.

```python
from notif import Client

launcher = Client("http://localhost:4010", ENROLLMENT_TOKEN)
worker, session = launcher.connect("research/scout", "Scout", ["message", "status"])
question = worker.ask(
    target={"kind": "person", "id": PERSON_ID},
    title="Which report format?",
    choices=["Brief", "Detailed"],
    key="report-42/format/v1",
)
# Continue other work, or wait now. A timeout never approves or cancels it.
result = worker.wait(question["id"], timeout=300)
```

For local source use, set `PYTHONPATH=sdk/python`. See [Go/Python/TypeScript and checkpoint examples](examples/README.md), [React embeds](sdk/react/README.md), and [API and integration contracts](docs/INTEGRATIONS.md). The module and package names are development publishing placeholders until the public namespace is finalized. Nothing has been published to registries.

## Included

- People, automatic agent enrollment, independent instances, revocable credentials, OIDC login, department/manager fields, groups and reusable roles.
- Directional View, Message, Ask, Decide and Manage grants, connection/action restrictions and bounded resource/amount conditions. Current policy is checked at sensitive boundaries.
- Private conversations, durable questions/approvals/tasks, independent pending requests, clarification, expiry/cancellation/supersession and exact action-bound execution intents.
- Outbound worker connections, fenced delivery leases, event replay, provider recovery and explicit uncertain outcomes.
- Self-hosted dashboard, employee inbox/chat, agent directory, access administration, activity/recovery surfaces and authenticated React embeds.
- Native web, signed incoming/outgoing webhooks, Slack messages/threads/verified interactive responses, and SMTP email with authenticated web response links.
- Exact scoped informational answer reuse, explicit publication/audiences/invalidation, optional reviewed semantic suggestions and a permission-filtered read-only fleet status view.
- REST, Go/TypeScript/Python SDKs, MCP stdio tools, CLI, versioned migrations, non-root Docker packaging and a tested restore reconciliation procedure.

Transport receipt, a human decision and worker-reported execution are distinct evidence. The engine cannot sandbox an arbitrary external agent or guarantee exactly-once effects in an unrelated API. Enforce permits at protected business tools and use destination idempotency/version fences. These boundaries are part of the API contract.

## Self-host and contribute

Start with [deployment and recovery](docs/OPERATIONS.md), [architecture](docs/ARCHITECTURE.md), [capability and benchmark coverage](docs/COVERAGE.md), [implementation checklist](docs/IMPLEMENTATION.md), and [contributing](CONTRIBUTING.md).

See the [byte and memory qualification](docs/BYTE-MEMORY-PERFORMANCE.md) for Linux measurements, exact correctness coverage and retained comparison tradeoffs.

```sh
docker compose up -d --wait
bash scripts/verify.sh
python3 scripts/recovery-drill.py
```

The verification command requires a real PostgreSQL database, runs the Go race suite and SDK/UI tests, builds the web packages, scans tracked files for credential patterns and checks Go vulnerabilities. The restore drill creates and drops only randomly named test databases in the local PostgreSQL container. CI is configured to run the same checks; a remote CI run requires publishing this repository.

Managed hosting, maintained enterprise directory reconciliation, billing, marketing/signup and commercial administration are separate workstreams built on this engine. Basic authentication, authorization, reliability and the usable self-hosted UI remain open source. [Optional assistance](docs/ASSISTANCE.md) supports explicit model configuration or custom gateways for answer reuse and confirmed overseer commands. SQLite and additional channel integrations remain outside this release. See the coverage matrix for live-provider and scale validation limits.
