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
  expect(screen.getByText(/NOTIF_TOKEN_FILE=/)).not.toHaveTextContent(
    "test-only-session",
  );
  expect(screen.getByText(/NOTIF_TOKEN_FILE=/)).not.toHaveTextContent(
    "NOTIF_AGENT_ID=",
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
  expect(screen.getByText(/\[mcp_servers.beepgrid\]/)).toHaveTextContent(
    'NOTIF_TOKEN_FILE = "/absolute/path/worker-token"',
  );
  expect(screen.getByText(/\[mcp_servers.beepgrid\]/)).not.toHaveTextContent(
    "test-only-secret-never-in-config",
  );
  expect(screen.getByText(/\[mcp_servers.beepgrid\]/)).not.toHaveTextContent(
    "NOTIF_AGENT_ID",
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
  fireEvent.click(screen.getByRole("radio", { name: "Queue" }));
  fireEvent.click(screen.getByRole("button", { name: "Create connection" }));
  await screen.findByRole("heading", { name: "Connect AI SDK" });
  expect(
    screen.queryByRole("heading", { name: "Run your worker" }),
  ).not.toBeInTheDocument();
  expect(
    screen.getByRole("radio", { name: "Queue" }),
  ).not.toBeVisible();
  fireEvent.click(
    screen.getByRole("button", { name: "Copy setup instruction" }),
  );
  await waitFor(() => expect(writeText).toHaveBeenCalledOnce());
  expect(writeText.mock.calls[0][0]).toContain("Queue");
  expect(writeText.mock.calls[0][0]).toContain("ins_live");
  expect(writeText.mock.calls[0][0]).not.toContain("secret-not-for-a-prompt");
  expect(writeText.mock.calls[0][0]).toContain("aiSDKBinding");
  fireEvent.click(screen.getByText("Change"));
  fireEvent.change(screen.getByRole("combobox", { name: "Your environment" }), {
    target: { value: "opencode" },
  });
  await screen.findByRole("heading", { name: "Connect OpenCode" });
  expect(
    screen.queryByRole("radio", { name: "Steer" }),
  ).not.toBeInTheDocument();
  expect(
    fn.mock.calls.filter(([path]) => path.endsWith("/sessions")),
  ).toHaveLength(1);
});
