import { useState } from "react";
import { Copy, Tabs } from "./ui";

export const connectionMethods = [
  { id: "python", label: "Python" },
  { id: "typescript", label: "TypeScript" },
  { id: "go", label: "Go" },
  { id: "mcp", label: "MCP tools" },
];
function quote(value: string) {
  return "'" + value.replaceAll("'", "'\\''") + "'";
}
export function connectionGuide(method: string, base: string, host = "codex") {
  const env = [
    "NOTIF_URL=" + quote(base) + " \\",
    "NOTIF_TOKEN_FILE='/absolute/path/worker-token' \\",
  ];
  if (method === "mcp") {
    return {
      prepare: "go build -buildvcs=false -o .local/beepgrid ./cmd/notif",
      run:
        host === "codex"
          ? [
              "[mcp_servers.beepgrid]",
              'command = "/absolute/path/beepgrid"',
              'args = ["mcp"]',
              "",
              "[mcp_servers.beepgrid.env]",
              "NOTIF_URL = " + JSON.stringify(base),
              'NOTIF_TOKEN_FILE = "/absolute/path/worker-token"',
            ].join("\n")
          : JSON.stringify(
              {
                mcpServers: {
                  beepgrid: {
                    command: "/absolute/path/beepgrid",
                    args: ["mcp"],
                    env: {
                      NOTIF_URL: base,
                      NOTIF_TOKEN_FILE: "/absolute/path/worker-token",
                    },
                  },
                },
              },
              null,
              2,
            ),
    };
  }
  return {
    prepare:
      method === "typescript"
        ? "npm --prefix sdk/typescript ci\nnpm --prefix sdk/typescript run build"
        : "",
    run: [
      ...env,
      method === "python"
        ? "PYTHONPATH=sdk/python python3 examples/python-worker/main.py"
        : method === "go"
          ? "go run -buildvcs=false ./examples/go-worker"
          : "node examples/typescript-worker/main.mjs",
    ].join("\n"),
  };
}

function Code({ value, label }: { value: string; label: string }) {
  return (
    <div className="setup-code">
      <div>
        <span>{label}</span>
        <Copy value={value} />
      </div>
      <pre className="code-example">{value}</pre>
    </div>
  );
}

export function AgentSetup({
  method,
  onMethodChange,
  base,
  token,
}: {
  method: string;
  onMethodChange: (method: string) => void;
  base: string;
  token: string;
}) {
  const [host, setHost] = useState("codex");
  const guide = connectionGuide(method, base, host);
  return (
    <div className="agent-setup-guide">
      <Tabs
        value={method}
        onChange={onMethodChange}
        options={connectionMethods}
      />
      <section className="setup-step">
        <header>
          <span>1</span>
          <h3>Save your connection token</h3>
        </header>
        <p>
          Copy it into a file outside your source code. This token expires after
          24 hours.
        </p>
        <div className="setup-token">
          <span>
            <code>worker-token</code>
            <small>Shown once. Keep this file private.</small>
          </span>
          <Copy value={token} label="Copy token" />
        </div>
        <Code
          label="Protect the token file"
          value="chmod 600 /absolute/path/worker-token"
        />
      </section>
      <section className="setup-step">
        <header>
          <span>2</span>
          <h3>
            {method === "mcp" ? "Connect your tool host" : "Run your worker"}
          </h3>
        </header>
        <p>
          From your Beepgrid source checkout. Use your token file’s absolute
          path below.
        </p>
        {guide.prepare ? (
          <Code
            label={
              method === "mcp"
                ? "Build the local MCP server"
                : "Prepare the SDK"
            }
            value={guide.prepare}
          />
        ) : null}
        {method === "mcp" ? (
          <>
            <Tabs
              value={host}
              onChange={setHost}
              options={[
                { id: "codex", label: "Codex" },
                { id: "json", label: "Other MCP clients" },
              ]}
            />
            <Code
              label={
                host === "codex"
                  ? "Add to ~/.codex/config.toml"
                  : "Add to your client’s MCP configuration"
              }
              value={guide.run}
            />
            <p className="help">
              Replace the server path with the binary you built. Restart your
              host, then ask it to receive and reply using Beepgrid. Tools do
              not wake an idle model.
            </p>
          </>
        ) : (
          <>
            <Code label="Start the connection" value={guide.run} />
            <p className="help">
              The starter receives messages and replies. Put your Agent’s logic
              in its handler; keep the worker running to receive new messages.
            </p>
          </>
        )}
        <details className="setup-reference">
          <summary>Connection details</summary>
          <dl>
            <dt>Workspace URL</dt>
            <dd>{base}</dd>
            <dt>Credential</dt>
            <dd>Existing worker connection. Do not enroll it again.</dd>
          </dl>
        </details>
      </section>
    </div>
  );
}
