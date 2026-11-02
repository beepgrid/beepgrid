import { useState } from "react";
import { Copy, Tabs } from "./ui";
import { ConnectionToken } from "./ConnectionToken";

function quote(value: string) {
  return "'" + value.replaceAll("'", "'\\''") + "'";
}
export function connectionGuide(method: string, base: string, host = "codex") {
  const env = [
    "BEEPGRID_URL=" + quote(base) + " \\",
    "BEEPGRID_TOKEN_FILE='/absolute/path/worker-token' \\",
  ];
  if (method === "mcp") {
    return {
      prepare:
        "brew install beepgrid/tap/beepgrid\n# Or, with Go installed:\ngo install github.com/beepgrid/beepgrid/cmd/beepgrid@latest",
      guide:
        "https://github.com/beepgrid/beepgrid/blob/main/docs/CONNECT-AGENT-RECIPES.md#local-mcp-host",
      run:
        host === "codex"
          ? [
              "[mcp_servers.beepgrid]",
              'command = "/absolute/path/beepgrid"',
              'args = ["mcp"]',
              "",
              "[mcp_servers.beepgrid.env]",
              "BEEPGRID_URL = " + JSON.stringify(base),
              'BEEPGRID_TOKEN_FILE = "/absolute/path/worker-token"',
            ].join("\n")
          : JSON.stringify(
              {
                mcpServers: {
                  beepgrid: {
                    command: "/absolute/path/beepgrid",
                    args: ["mcp"],
                    env: {
                      BEEPGRID_URL: base,
                      BEEPGRID_TOKEN_FILE: "/absolute/path/worker-token",
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
        ? "npm install @beepgrid/sdk"
        : method === "go"
          ? "go get github.com/beepgrid/beepgrid/sdk/go@latest"
          : "python3 -m pip install beepgrid\n# Or, in a uv project:\nuv add beepgrid",
    guide: `https://github.com/beepgrid/beepgrid/blob/main/docs/CONNECT-AGENT-RECIPES.md#${method}`,
    run: [
      ...env,
      method === "python"
        ? "python3 worker.py"
        : method === "go"
          ? "go run ."
          : "node worker.mjs",
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

export function WorkerSetup({
  method,
  base,
  token,
}: {
  method: string;
  base: string;
  token: string;
}) {
  const [host, setHost] = useState("codex");
  const guide = connectionGuide(method, base, host);
  return (
    <div className="agent-setup-guide">
      <section className="setup-step">
        <header>
          <span>1</span>
          <h3>Save your connection token</h3>
        </header>
        <p>
          Copy it into a file outside your source code. Manage its expiry in
          Settings → Access tokens.
        </p>
        <ConnectionToken token={token} />
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
          {method === "mcp"
            ? "Install the Beepgrid executable, then use its absolute path and your token file’s path below."
            : "Install the SDK in your Agent project. Save the starter from the guide there, then use your token file’s absolute path below."}
        </p>
        {guide.prepare ? (
          <Code
            label={
              method === "mcp" ? "Install the MCP server" : "Install the SDK"
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
              Replace the server path with your installed binary. Restart your
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
        <a
          className="button"
          href={guide.guide}
          target="_blank"
          rel="noreferrer"
        >
          Open integration guide
        </a>
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
