# Connect Agent: minimal supported setup

For live input during an existing Agent task, choose a harness/framework in the
workspace and follow [Live input](LIVE-INPUT.md). The sequential worker and
tool-only paths below remain available under Advanced.

The dashboard creates one worker connection and supplies its credential. Use that worker credential directly; omit `BEEPGRID_AGENT_ID`. Do not ask the developer to create a Service, launcher or second Agent for this path. Select the URL of the same workspace and store the credential in an owner-only file or the runtime's secret manager.

## Common environment

```sh
export BEEPGRID_URL="https://your-workspace.example"
export BEEPGRID_TOKEN_FILE="/absolute/path/beepgrid-worker-token"
chmod 600 "$BEEPGRID_TOKEN_FILE"
```

The starter validates `/api/v1/me` as an Agent with an instance ID, prints that identity and runs receive/reply. It never silently enrolls. A successful API write confirms canonical queueing; use an actual reply to prove the running process.

## Use the published SDKs

No Beepgrid checkout is required. Work in the project that runs your Agent.
Choose a language below, install its SDK, and save the starter under the indicated
filename. These examples send a deterministic reply so you can verify the
connection before replacing the handler with your Agent's own logic. They do not
run a model for you. Leave `BEEPGRID_AGENT_ID` unset to reuse the supplied worker:

```sh
unset BEEPGRID_AGENT_ID
```

## Python

Use Python 3.10 or later.

```sh
python3 -m pip install beepgrid
# Or, in a uv project:
uv add beepgrid
```

Save as `worker.py`:

```python
"""Use an existing worker credential, or explicitly enroll with BEEPGRID_AGENT_ID."""
import os
from pathlib import Path
from beepgrid import Client

token = os.environ.get("BEEPGRID_TOKEN", "")
if os.environ.get("BEEPGRID_TOKEN_FILE"):
    path = Path(os.environ["BEEPGRID_TOKEN_FILE"])
    if path.stat().st_mode & 0o077:
        raise RuntimeError("Credential file must be owner-only (chmod 600)")
    token = path.read_text().strip()
if not token:
    raise RuntimeError("Set BEEPGRID_TOKEN_FILE or BEEPGRID_TOKEN")
worker = Client(os.environ.get("BEEPGRID_URL", "http://localhost:4010"), token)
agent_id = os.environ.get("BEEPGRID_AGENT_ID", "").strip()
if agent_id:
    worker, session = worker.connect(agent_id, os.environ.get("BEEPGRID_AGENT_NAME", "Python worker"), ["message"])
    identity = {"identity_type": "agent", "agent": session["agent"], "instance_id": session["instance_id"]}
else:
    identity = worker.call("GET", "/api/v1/me")
if identity.get("identity_type") != "agent" or not identity.get("agent", {}).get("id") or not identity.get("instance_id"):
    raise RuntimeError("Without BEEPGRID_AGENT_ID, supply an existing worker credential for an agent connection")
print("Agent:", identity["agent"]["id"], "Instance:", identity["instance_id"], flush=True)

def handle(delivery):
    if delivery["payload"]["type"] == "message":
        message = delivery["payload"]["message"]
        worker.send(conversation_id=message["conversation_id"],
                    body="Python worker received: " + message["body"], key="reply/" + delivery["id"])

worker.run(handle)
```

With the common environment set, run:

```sh
python3 worker.py
```

## TypeScript

Use Node.js 22.13 or later. The `.mjs` starter imports the published ESM package.

```sh
npm install @beepgrid/sdk
```

Save as `worker.mjs`:

```javascript
import { readFileSync, statSync } from "node:fs";
let token=process.env.BEEPGRID_TOKEN;
if(process.env.BEEPGRID_TOKEN_FILE){
  if(statSync(process.env.BEEPGRID_TOKEN_FILE).mode & 0o077)throw new Error("Credential file must be owner-only (chmod 600)");
  token=readFileSync(process.env.BEEPGRID_TOKEN_FILE,"utf8").trim();
}
if(!token)throw new Error("Set BEEPGRID_TOKEN_FILE or BEEPGRID_TOKEN");
import { Client } from "@beepgrid/sdk";
let client = new Client(
  process.env.BEEPGRID_URL ?? "http://localhost:4010",
  token,
);
const agentId = process.env.BEEPGRID_AGENT_ID?.trim();
let identity;
if (agentId) {
  const connected = await client.connect(agentId, process.env.BEEPGRID_AGENT_NAME ?? "TypeScript worker", ["message"]);
  client = connected.client;
  identity = {identity_type: "agent", agent: connected.session.agent, instance_id: connected.session.instance_id};
} else {
  identity = await client.call("GET", "/api/v1/me");
}
if (identity.identity_type !== "agent" || !identity.agent?.id || !identity.instance_id) {
  throw new Error("Without BEEPGRID_AGENT_ID, supply an existing worker credential for an agent connection");
}
console.log("Agent:", identity.agent.id, "Instance:", identity.instance_id);
const controller = new AbortController();
process.on("SIGINT", () => controller.abort());
process.on("SIGTERM", () => controller.abort());
await client.run(async (delivery) => {
  if (delivery.payload.type === "message") {
    const m = delivery.payload.message;
    await client.send(
      {
        conversation_id: m.conversation_id,
        body: "TypeScript worker received: " + m.body,
      },
      "reply/" + delivery.id,
    );
  }
}, controller.signal);
```

With the common environment set, run:

```sh
node worker.mjs
```

## Go

In a new Go project, run `go mod init example.com/my-agent` first.

```sh
go get github.com/beepgrid/beepgrid/sdk/go@latest
```

Save as `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/beepgrid/beepgrid/sdk/go"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	token := os.Getenv("BEEPGRID_TOKEN")
	if file := os.Getenv("BEEPGRID_TOKEN_FILE"); file != "" {
		info, err := os.Stat(file)
		if err != nil {
			panic(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			panic("credential file must be owner-only (chmod 600)")
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			panic(err)
		}
		token = strings.TrimSpace(string(raw))
	}
	if token == "" {
		panic("set BEEPGRID_TOKEN_FILE or BEEPGRID_TOKEN")
	}
	c := beepgrid.New(value("BEEPGRID_URL", "http://localhost:4010"), token)
	var identity struct {
		Kind       string      `json:"identity_type"`
		Agent      beepgrid.Agent `json:"agent"`
		InstanceID string      `json:"instance_id"`
	}
	if agent := strings.TrimSpace(os.Getenv("BEEPGRID_AGENT_ID")); agent != "" {
		worker, session, err := c.Enroll(ctx, beepgrid.EnrollInput{Agent: agent, Name: value("BEEPGRID_AGENT_NAME", "Go worker"), Capabilities: []string{"message"}})
		if err != nil {
			panic(err)
		}
		c = worker
		identity.Kind, identity.Agent, identity.InstanceID = "agent", session.Agent, session.InstanceID
	} else {
		if err := c.Do(ctx, "GET", "/api/v1/me", nil, &identity, ""); err != nil {
			panic(err)
		}
	}
	if identity.Kind != "agent" || identity.Agent.ID == "" || identity.InstanceID == "" {
		panic("without BEEPGRID_AGENT_ID, supply an existing worker credential for an agent connection")
	}
	fmt.Println("Agent:", identity.Agent.ID, "Instance:", identity.InstanceID)
	err := c.Run(ctx, func(ctx context.Context, d beepgrid.Delivery) error {
		if d.Payload.Type == "message" && d.Payload.Message != nil {
			_, e := c.Send(ctx, beepgrid.SendInput{ConversationID: d.Payload.Message.ConversationID, Body: "Go worker received: " + d.Payload.Message.Body}, "reply/"+d.ID)
			return e
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		panic(err)
	}
}

func value(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
```

With the common environment set, run:

```sh
go run .
```

## Local MCP host

Install the executable through Homebrew or Go:

```sh
brew install beepgrid/tap/beepgrid
# Or, with Go installed:
go install github.com/beepgrid/beepgrid/cmd/beepgrid@latest
```

For Windows or a machine without either tool, use a [prebuilt binary](PACKAGING.md).
Find the installed executable with `command -v beepgrid` (`Get-Command beepgrid`
in PowerShell). Configure the host to spawn that absolute path with argument
`mcp` and the same two environment variables. No database or source checkout is
needed on this client machine. For Codex:

```toml
[mcp_servers.beepgrid]
command = "/absolute/path/beepgrid"
args = ["mcp"]

[mcp_servers.beepgrid.env]
BEEPGRID_URL = "https://your-workspace.example"
BEEPGRID_TOKEN_FILE = "/absolute/path/beepgrid-worker-token"
```

The host initializes, lists tools and calls `receive`; it sends through `send` and acknowledges only after handling the delivery. This is a local stdio process. Adding tools alone does not wake an idle model. Remote authenticated MCP and third-party model execution are separately qualified work.

## Connection proof and advanced options

The live runtime prints the exact Agent and instance ID from its original worker token. The six Go/Python/TypeScript starter cases test direct-worker and explicit-enrollment modes over real HTTP, including exact-instance delivery, canonical reply attribution, unchanged replica count and a decoy replica that cannot claim the addressed message. CLI and local MCP run as actual binaries against disposable PostgreSQL state. No hosted model runtime is inferred.

Only show enrollment instructions when intentionally selected: a namespace-restricted launcher credential plus `BEEPGRID_AGENT_ID` enrolls a new worker. `BEEPGRID_AGENT_NAME` alone does not select enrollment.

The new SDK file and membership helpers add no connection setup fields. They expose private `stageAttachment`/`stage_attachment`/`StageAttachment`, `attachment_ids` (`AttachmentIDs` in Go), and versioned `changeChatMembership`/`change_chat_membership`/`ChangeChatMembership`. Earlier history is shared only with explicit `share_history: true`. The dashboard guide can keep these out of the initial connection steps.
