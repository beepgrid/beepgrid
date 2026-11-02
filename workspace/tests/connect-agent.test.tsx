import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Connect } from "../src/pages/Directory";
import { harness, agent, json, requestBody } from "./fixtures";
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
});
it("uses the existing worker credential and verifies its exact instance reply", async () => {
  let replied = false;
  const fn = vi.fn(async (path: string, _init?: RequestInit) =>
    path.endsWith("/sessions")
      ? json({ token: "test-only-session", agent, instance_id: "ins_new" })
      : path.endsWith("/messages")
        ? json({ conversation_id: "con_check" })
        : json({
            items: [
              {
                id: replied ? "msg_new" : "msg_old",
                sender_id: agent.id,
                instance_id: replied ? "ins_new" : "ins_old",
                body: "Reply",
              },
            ],
          }),
  );
  vi.stubGlobal("fetch", fn);
  const { Wrapper, query } = harness();
  render(<Connect agent={{ ...agent, kind: "agent" }} onClose={vi.fn()} />, {
    wrapper: Wrapper,
  });
  fireEvent.change(screen.getByRole("combobox", { name: "Your environment" }), {
    target: { value: "python" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create connection" }));
  await screen.findByRole("heading", { name: "Run your worker" });
  expect(screen.getByText(/python3 -m pip install beepgrid/)).toBeVisible();
  expect(screen.getByText(/BEEPGRID_TOKEN_FILE=/)).toHaveTextContent(
    "python3 worker.py",
  );
  expect(
    screen.getByRole("link", { name: "Open integration guide" }),
  ).toHaveAttribute(
    "href",
    "https://github.com/beepgrid/beepgrid/blob/main/docs/CONNECT-AGENT-RECIPES.md#python",
  );
  expect(screen.getByText(/BEEPGRID_TOKEN_FILE=/)).not.toHaveTextContent(
    "test-only-session",
  );
  expect(screen.getByText(/BEEPGRID_TOKEN_FILE=/)).not.toHaveTextContent(
    "BEEPGRID_AGENT_ID=",
  );
  fireEvent.click(screen.getByRole("button", { name: "Send a test message" }));
  await screen.findByText("Waiting for your worker’s reply...");
  const call = fn.mock.calls.find(([p]) => p.endsWith("/messages"))!;
  expect(requestBody(call[1]).target).toEqual({
    kind: "instance",
    id: "ins_new",
  });
  expect(screen.queryByText(/Reply received from/)).not.toBeInTheDocument();
  replied = true;
  await act(() => query.invalidateQueries());
  await waitFor(() =>
    expect(
      screen.getByText("Reply received from Research assistant."),
    ).toBeInTheDocument(),
  );
});

it("guides the chosen MCP method without exposing a token in configuration or reenrolling", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      path.endsWith("/sessions")
        ? json({
            token: "test-only-secret-never-in-config",
            agent,
            instance_id: "ins_mcp",
          })
        : json({ items: [] }),
    ),
  );
  const { Wrapper } = harness();
  render(<Connect agent={{ ...agent, kind: "agent" }} onClose={vi.fn()} />, {
    wrapper: Wrapper,
  });
  fireEvent.change(screen.getByRole("combobox", { name: "Your environment" }), {
    target: { value: "mcp" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create connection" }));
  await screen.findByRole("heading", { name: "Connect your tool host" });
  expect(
    screen.getByText(/brew install beepgrid\/tap\/beepgrid/),
  ).toBeVisible();
  expect(
    screen.getByRole("link", { name: "Open integration guide" }),
  ).toHaveAttribute(
    "href",
    "https://github.com/beepgrid/beepgrid/blob/main/docs/CONNECT-AGENT-RECIPES.md#local-mcp-host",
  );
  expect(screen.getByText(/\[mcp_servers.beepgrid\]/)).toHaveTextContent(
    'BEEPGRID_TOKEN_FILE = "/absolute/path/worker-token"',
  );
  expect(screen.getByText(/\[mcp_servers.beepgrid\]/)).not.toHaveTextContent(
    "test-only-secret-never-in-config",
  );
  expect(screen.getByText(/\[mcp_servers.beepgrid\]/)).not.toHaveTextContent(
    "BEEPGRID_AGENT_ID",
  );
  expect(screen.getByText(/Tools do not wake an idle model/)).toBeVisible();
  fireEvent.click(screen.getByRole("tab", { name: "Other MCP clients" }));
  expect(screen.getByText(/"mcpServers"/)).toHaveTextContent(
    '"args": [ "mcp" ]',
  );
});

it("reveals only the chosen live integration and keeps credentials out of its setup instruction", async () => {
  const writeText = vi.fn(async (_text: string) => {});
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
  const fn = vi.fn(async (path: string) =>
    path.endsWith("/sessions")
      ? json({
          token: "secret-not-for-a-prompt",
          agent,
          instance_id: "ins_live",
        })
      : json({ items: [] }),
  );
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(<Connect agent={{ ...agent, kind: "agent" }} onClose={vi.fn()} />, {
    wrapper: Wrapper,
  });
  expect(
    screen.getByRole("button", { name: "Create connection" }),
  ).toBeDisabled();
  expect(
    screen.queryByRole("radio", { name: "Steer" }),
  ).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole("combobox", { name: "Your environment" }), {
    target: { value: "ai-sdk" },
  });
  expect(screen.getByRole("radio", { name: "Steer" })).toBeChecked();
  expect(
    screen.getByRole("group", { name: "When the agent is busy, default to" }),
  ).toBeVisible();
  expect(
    screen.queryByText(/Steering is not supported/),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  const info = screen.getByRole("button", { name: "About Steer and Queue" });
  fireEvent.focus(info);
  expect(screen.getByRole("tooltip")).toHaveTextContent(
    "Steer uses new input during ongoing work",
  );
  expect(screen.getByRole("tooltip")).toHaveTextContent(
    "Queue handles new messages after current work finishes",
  );
  fireEvent.keyDown(info, { key: "Escape" });
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  fireEvent.mouseEnter(info);
  expect(screen.getByRole("tooltip")).toBeVisible();
  fireEvent.mouseLeave(info);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("radio", { name: "Queue" }));
  fireEvent.click(screen.getByRole("button", { name: "Create connection" }));
  await screen.findByRole("heading", { name: "Instructions for your AI" });
  expect(screen.getByRole("heading", { name: "Set up by hand" })).toBeVisible();
  expect(
    screen.getByRole("link", { name: "Open integration guide" }),
  ).toBeVisible();
  expect(
    screen.getByRole("link", { name: "Open integration guide" }),
  ).toHaveAttribute(
    "href",
    "https://github.com/beepgrid/beepgrid/blob/main/docs/LIVE-INPUT.md#manual-setup",
  );
  expect(
    screen.queryByRole("button", { name: "Set up by hand" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "Run your worker" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("radio", { name: "Queue" })).toBeVisible();
  fireEvent.click(
    screen.getByRole("button", { name: "Copy setup instruction" }),
  );
  await waitFor(() => expect(writeText).toHaveBeenCalledOnce());
  expect(writeText.mock.calls[0][0]).toContain("Queue");
  expect(writeText.mock.calls[0][0]).toContain("ins_live");
  expect(writeText.mock.calls[0][0]).not.toContain("secret-not-for-a-prompt");
  expect(writeText.mock.calls[0][0]).toContain("aiSDKBinding");
  expect(writeText.mock.calls[0][0]).toContain(
    "https://github.com/beepgrid/beepgrid#readme",
  );
  const token = screen.getByLabelText("Connection token");
  expect(token).toHaveAttribute("type", "password");
  expect(token).toHaveValue("secret-not-for-a-prompt");
  fireEvent.click(screen.getByRole("button", { name: "Show" }));
  expect(token).toHaveAttribute("type", "text");
  fireEvent.click(screen.getByRole("button", { name: "Hide" }));
  fireEvent.click(screen.getByRole("button", { name: "Copy token" }));
  await waitFor(() =>
    expect(writeText).toHaveBeenLastCalledWith("secret-not-for-a-prompt"),
  );
  expect(writeText.mock.calls[0][0]).toContain(
    "https://github.com/beepgrid/beepgrid/blob/main/docs/LIVE-INPUT.md#ai-sdk",
  );
  expect(writeText.mock.calls[0][0]).toContain("Ask me for the absolute path");
  expect(screen.queryByText("Change")).not.toBeInTheDocument();
  expect(
    screen.queryByText("Generate the instruction locally"),
  ).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole("combobox", { name: "Your environment" }), {
    target: { value: "opencode" },
  });
  await screen.findByRole("heading", { name: "Instructions for your AI" });
  expect(
    screen.queryByRole("radio", { name: "Steer" }),
  ).not.toBeInTheDocument();
  expect(
    screen.getByText("Steering is not supported with this environment."),
  ).toBeVisible();
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  expect(
    fn.mock.calls.filter(([path]) => path.endsWith("/sessions")),
  ).toHaveLength(1);
});
