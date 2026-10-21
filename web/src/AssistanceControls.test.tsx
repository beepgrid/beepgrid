// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  AssistanceControls,
  disabledAssistance,
  OverseerPanel,
} from "./AssistanceControls";

const source = {
  agent: { id: "agt_1", name: "Scout" },
  fresh: true,
  instances: [
    { progress: { completed: 7 }, last_seen: new Date().toISOString() },
  ],
};
const base = {
  id: "ovr_1",
  prompt: "Ask for progress",
  operation: "question",
  summary: "Ask Scout for an update",
  body: "What is complete?",
  context_agent_ids: ["agt_1"],
  sources: [source],
  requires_confirmation: true,
  created_at: new Date().toISOString(),
  expires_at: new Date(Date.now() + 600000).toISOString(),
  dispatches: [],
  replies: [],
};
let calls: { path: string; body: unknown }[];
beforeEach(() => {
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url, init) => {
      const path = String(url);
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ path, body });
      const data = path.endsWith("/fleet/plans")
        ? []
        : path.endsWith("/summary")
          ? {
              ...base,
              requires_confirmation: false,
              reply_summary: "Scout reports seven pages complete.",
              dispatches: [
                {
                  agent_id: "agt_1",
                  conversation_id: "con_1",
                  request_id: "req_1",
                },
              ],
              replies: [
                {
                  id: "req_1",
                  state: "resolved",
                  response: { answer: "Seven pages" },
                },
              ],
            }
          : path.endsWith("/command") || path.endsWith("/plans/ovr_1")
            ? {
                ...base,
                requires_confirmation: false,
                dispatches: [
                  {
                    agent_id: "agt_1",
                    conversation_id: "con_1",
                    request_id: "req_1",
                  },
                ],
                replies: [
                  {
                    id: "req_1",
                    title: "Scout progress",
                    state: "resolved",
                    response: { answer: "Seven pages" },
                  },
                ],
              }
            : base;
      return new Response(JSON.stringify(data), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("optional assistance", () => {
  it("does not race manual and automatic reply summaries into duplicate model calls", async () => {
    const originalFetch = fetch;
    let releaseSummary: (() => void) | undefined;
    vi.stubGlobal(
      "fetch",
      async (url: RequestInfo | URL, init?: RequestInit) => {
        const response = await originalFetch(url, init);
        if (String(url).endsWith("/summary"))
          await new Promise<void>((resolve) => {
            releaseSummary = resolve;
          });
        return response;
      },
    );
    render(<OverseerPanel groups={[]} visibleAgentIDs={["agt_1"]} />);
    fireEvent.change(
      screen.getByRole("textbox", { name: "Ask the overseer" }),
      { target: { value: "Ask for progress" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Ask overseer" }));
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Confirm and send to 1 agent",
      }),
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "Summarize replies" }),
    );
    await new Promise((resolve) => setTimeout(resolve, 1250));
    expect(calls.filter((call) => call.path.endsWith("/summary"))).toHaveLength(
      1,
    );
    releaseSummary?.();
    expect(
      await screen.findByText("Scout reports seven pages complete."),
    ).toBeVisible();
  });

  it("defaults off, explains configuration and sends no request until explicitly toggled", async () => {
    const act = vi.fn(async (task) => {
      await task();
      return true;
    });
    const { rerender } = render(
      <AssistanceControls settings={disabledAssistance} act={act} />,
    );
    expect(
      screen.getByRole("checkbox", { name: "Automatic answer reuse" }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "Overseer assistant" }),
    ).toBeDisabled();
    expect(calls).toHaveLength(0);
    rerender(
      <AssistanceControls
        settings={{
          ...disabledAssistance,
          matcher_configured: true,
          overseer_configured: true,
        }}
        act={act}
      />,
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Overseer assistant" }),
    );
    await waitFor(() => expect(calls[0]?.body).toEqual({ overseer: true }));
  });
  it("previews authorized recipients, confirms only stored plan ID and summarizes actual replies", async () => {
    render(<OverseerPanel groups={[]} visibleAgentIDs={["agt_1"]} />);
    fireEvent.change(
      screen.getByRole("textbox", { name: "Ask the overseer" }),
      { target: { value: "Ask for progress" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Ask overseer" }));
    expect(await screen.findByText("What is complete?")).toBeVisible();
    expect(calls.some((c) => c.path.endsWith("/command"))).toBe(false);
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm and send to 1 agent" }),
    );
    expect(await screen.findByText("Seven pages")).toBeVisible();
    expect(calls.find((c) => c.path.endsWith("/command"))?.body).toEqual({
      plan_id: "ovr_1",
    });
    await waitFor(
      () =>
        expect(
          screen.getByText("Scout reports seven pages complete."),
        ).toBeVisible(),
      { timeout: 3000 },
    );
  });
  it("removes derived text and confirmations when access to any source is revoked", async () => {
    const { rerender } = render(
      <OverseerPanel groups={[]} visibleAgentIDs={["agt_1"]} />,
    );
    fireEvent.change(
      screen.getByRole("textbox", { name: "Ask the overseer" }),
      { target: { value: "Status" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Ask overseer" }));
    expect(await screen.findByText(base.summary)).toBeVisible();
    rerender(<OverseerPanel groups={[]} visibleAgentIDs={[]} />);
    await waitFor(() =>
      expect(screen.queryByText(base.summary)).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Access changed");
    expect(calls.some((c) => c.path.endsWith("/command"))).toBe(false);
  });
  it("shows errors without silently enabling or sending", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              error: { code: "DENIED", message: "Access changed" },
            }),
            { status: 403 },
          ),
      ),
    );
    render(<OverseerPanel groups={[]} visibleAgentIDs={["agt_1"]} />);
    fireEvent.change(
      screen.getByRole("textbox", { name: "Ask the overseer" }),
      { target: { value: "Status" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Ask overseer" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Access changed",
    );
    expect(screen.queryByText("Confirm and send")).not.toBeInTheDocument();
  });
});
