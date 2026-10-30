import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import Overseer from "../src/pages/Overseer";
import Conversations from "../src/pages/Conversations";
import { OverseerEntry } from "../src/OverseerEntry";
import { applyLiveEvent } from "../src/live-cache";
import type { Plan } from "../src/contracts";
import { agent, harness, json, me, requestBody } from "./fixtures";

const settings = {
  overseer: true,
  overseer_configured: true,
  version: 2,
};
function plan(id = "ovr_first"): Plan {
  return {
    id,
    prompt: "How is Scout doing?",
    operation: "status",
    summary: "Scout reports three of ten pages complete.",
    sources: [
      {
        agent: agent as Plan["sources"][0]["agent"],
        instances: [],
        fresh: true,
      },
    ],
    requires_confirmation: false,
    expires_at: new Date(Date.now() + 600000).toISOString(),
    created_at: "2026-10-26T00:00:00Z",
    dispatches: [],
    replies: [],
  };
}
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  history.replaceState(null, "", "/agents");
});

it("does not claim message acceptance for an informational reply without dispatch", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) =>
      path.endsWith("/assistance/settings")
        ? json(settings)
        : json({
            items:
              requestBody(init).collection === "overseer_plans" ? [plan()] : [],
          }),
    ),
  );
  const { Wrapper } = harness();
  render(<Overseer />, { wrapper: Wrapper });
  await screen.findByText("Scout reports three of ten pages complete.");
  expect(screen.queryByText(/messages accepted/)).not.toBeInTheDocument();
  expect(screen.queryByText(/0 agents/)).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Open conversation" }),
  ).not.toBeInTheDocument();
});

it("keeps a 25-agent result compact and reveals canonical per-agent details on demand", async () => {
  const large = plan();
  large.dispatches = Array.from({ length: 25 }, (_, i) => ({
    agent_id: `agt_${i}`,
    conversation_id: `con_${i}`,
    request_id: `req_${i}`,
  }));
  large.replies = Array.from({ length: 25 }, (_, i) => ({
    id: `req_${i}`,
    state: i < 3 ? "resolved" : "open",
    title: `Agent ${i}`,
    response:
      i < 3 ? { answer: `Result ${i}`, outcome: "answered" } : undefined,
  })) as Plan["replies"];
  const fetcher = vi.fn(async (path: string, init?: RequestInit) =>
    path.endsWith("/assistance/settings")
      ? json(settings)
      : path.endsWith("/query")
        ? json({
            items:
              requestBody(init).collection === "overseer_plans" ? [large] : [],
          })
        : json({}),
  );
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(<Overseer />, { wrapper: Wrapper });
  const summary = await screen.findByText(
    "25 agents · 3 of 25 replies received · Details",
  );
  expect(screen.queryByText("Result 0")).not.toBeInTheDocument();
  fireEvent.click(summary);
  await screen.findByText("Result 0");
  expect(
    screen.getAllByRole("button", { name: "Open conversation" }),
  ).toHaveLength(25);
  expect(screen.getAllByText("Awaiting an authorized reply")).toHaveLength(22);
});

it("opens the explicit assistant without enrolling it or querying a fake identity", async () => {
  const fetcher = vi.fn(async (_path: string) => json(settings));
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(<OverseerEntry />, { wrapper: Wrapper });
  await screen.findByText("On");
  fireEvent.click(screen.getByRole("button", { name: /Coordinator/ }));
  expect(location.pathname).toBe("/conversations/overseer");
  expect(
    fetcher.mock.calls.every(
      ([path]) => path === "/api/v1/assistance/settings",
    ),
  ).toBe(true);
});
it("shows a truthful setup state with assistance off and makes no model/history calls", async () => {
  const fetcher = vi.fn(async (_path: string) =>
    json({ ...settings, overseer: false, overseer_configured: false }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(<Overseer />, { wrapper: Wrapper });
  await screen.findByText(
    "Ask a workspace administrator to enable Coordinator.",
  );
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  expect(
    fetcher.mock.calls.every(
      ([path]) => path === "/api/v1/assistance/settings",
    ),
  ).toBe(true);
});
it("lets a Coordinator starter fill a draft without sending it", async () => {
  const fetcher = vi.fn(async (path: string) =>
    path.endsWith("/assistance/settings")
      ? json(settings)
      : json({ items: [] }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(<Overseer />, { wrapper: Wrapper });
  fireEvent.click(
    await screen.findByRole("button", { name: "Check release blockers" }),
  );
  expect(
    screen.getByRole("textbox", { name: "Message Coordinator" }),
  ).toHaveValue(
    "Find out which agents are waiting on release blockers and summarize what needs my attention.",
  );
  expect(
    screen.getByRole("textbox", { name: "Message Coordinator" }),
  ).toHaveFocus();
  expect(
    fetcher.mock.calls.some(([path]) => path.endsWith("/fleet/coordinate")),
  ).toBe(false);
});
it("reloads private chronological turns, sends bounded follow-up identity and clears only accepted drafts", async () => {
  let turns = [plan()];
  const posts: { path: string; body: any }[] = [];
  const fetcher = vi.fn(async (path: string, init?: RequestInit) => {
    const body = requestBody(init);
    if (init?.method === "POST") posts.push({ path, body });
    if (path.endsWith("/assistance/settings")) return json(settings);
    if (path.endsWith("/groups")) return json([]);
    if (path.endsWith("/query"))
      return json({
        items: body.collection === "groups" ? [] : turns,
        order: "created_at_id_descending",
      });
    if (path.endsWith("/fleet/coordinate")) {
      const next = {
        ...plan("ovr_follow_" + turns.length),
        prompt: body.prompt,
        previous_plan_id: body.previous_plan_id,
      };
      turns = [next, ...turns];
      return json(next);
    }
    return json({});
  });
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  const view = render(<Overseer />, { wrapper: Wrapper });
  await screen.findByText("Scout reports three of ten pages complete.");
  fireEvent.change(
    screen.getByRole("textbox", { name: "Message Coordinator" }),
    {
      target: { value: "Explain that" },
    },
  );
  fireEvent.click(screen.getByRole("button", { name: "Send to Coordinator" }));
  await waitFor(() =>
    expect(
      posts.find((p) => p.path.endsWith("/fleet/coordinate"))?.body,
    ).toEqual({
      prompt: "Explain that",
      previous_plan_id: "ovr_first",
    }),
  );
  await waitFor(() =>
    expect(
      screen.getByRole("textbox", { name: "Message Coordinator" }),
    ).toHaveValue(""),
  );
  expect(posts.some((p) => p.path.endsWith("/fleet/command"))).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "New topic" }));
  fireEvent.change(
    screen.getByRole("textbox", { name: "New topic for Coordinator" }),
    { target: { value: "New question" } },
  );
  fireEvent.click(screen.getByRole("button", { name: "Send to Coordinator" }));
  await waitFor(() =>
    expect(
      posts.filter((p) => p.path.endsWith("/fleet/coordinate")).at(-1)?.body,
    ).toEqual({ prompt: "New question" }),
  );
  view.unmount();
  const next = harness();
  render(<Overseer />, { wrapper: next.Wrapper });
  await screen.findByText("Explain that");
  expect(
    posts
      .filter((p) => p.path.endsWith("/query"))
      .every((p) => ["overseer_plans", "groups"].includes(p.body.collection)),
  ).toBe(true);
});
it("keeps a proposed exact instruction separate from direct chat and requires explicit confirmation", async () => {
  let turn: Plan = {
    ...plan(),
    operation: "question",
    body: "Report current progress.",
    summary: "Ask Scout for an update.",
    requires_confirmation: true,
  };
  const mutations: { path: string; body: any }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (path.endsWith("/assistance/settings")) return json(settings);
      if (path.endsWith("/groups")) return json([]);
      if (path.endsWith("/query"))
        return json({ items: [turn], order: "created_at_id_descending" });
      mutations.push({ path, body: requestBody(init) });
      turn = {
        ...turn,
        requires_confirmation: false,
        dispatches: [
          {
            agent_id: agent.id,
            conversation_id: "con_direct",
            request_id: "req_one",
          },
        ],
        replies: [],
      };
      return json(turn);
    }),
  );
  const { Wrapper } = harness();
  render(<Overseer />, { wrapper: Wrapper });
  await screen.findByText("Review exact instruction");
  expect(mutations).toHaveLength(0);
  expect(screen.getByText("Report current progress.")).toBeInTheDocument();
  fireEvent.click(
    screen.getByRole("button", { name: "Confirm exact instruction" }),
  );
  await screen.findByText("Accepted instruction");
  expect(mutations).toEqual([
    { path: "/api/v1/fleet/command", body: { plan_id: "ovr_first" } },
  ]);
  const details = screen
    .getByText("1 agent · messages accepted · Details")
    .closest("details")!;
  details.open = true;
  fireEvent(details, new Event("toggle"));
  fireEvent.click(
    await screen.findByRole("button", { name: "Open conversation" }),
  );
  expect(location.pathname).toBe("/conversations/con_direct");
});
it("removes saved content on current read failure or assistance disablement", async () => {
  let failure = false;
  let enabled = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path.endsWith("/assistance/settings"))
        return json({ ...settings, overseer: enabled });
      if (path.endsWith("/groups")) return json([]);
      return failure
        ? json({ error: { message: "Access changed" } }, 404)
        : json({ items: [plan()], order: "created_at_id_descending" });
    }),
  );
  const { Wrapper, query } = harness();
  render(<Overseer />, { wrapper: Wrapper });
  await screen.findByText("Scout reports three of ten pages complete.");
  failure = true;
  await act(() => query.invalidateQueries());
  await screen.findByText("Access changed");
  expect(
    screen.queryByText("Scout reports three of ten pages complete."),
  ).not.toBeInTheDocument();
  enabled = false;
  await act(() => query.invalidateQueries());
  await screen.findByText(
    "Ask a workspace administrator to enable Coordinator.",
  );
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
});
it("ordinary multi-agent conversation sends directly even with assistance enabled", async () => {
  history.replaceState(null, "", "/conversations/con_team");
  const mutations: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (path.endsWith("/assistance/settings")) return json(settings);
      if (path.endsWith("/query"))
        return json({ items: [], order: "id_ascending" });
      if (path.endsWith("/capabilities"))
        return json({
          operations: { view: true, message: true },
          outcomes: [],
        });
      if (path.endsWith("/messages")) {
        mutations.push(path);
        return json({ messages: [], conversation_id: "con_team" });
      }
      return json({
        id: "con_team",
        title: "Three agents",
        participants: [
          me.person,
          agent,
          { ...agent, id: "agt_builder", name: "Builder" },
          { ...agent, id: "agt_reviewer", name: "Reviewer" },
        ],
        messages: [],
      });
    }),
  );
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  fireEvent.change(await screen.findByRole("textbox", { name: "Message" }), {
    target: { value: "Hello team" },
  });
  fireEvent.click(screen.getByRole("button", { name: /^Send$/ }));
  await waitFor(() => expect(mutations).toEqual(["/api/v1/messages"]));
});
it("refreshes saved assistant turns on reply and assistant events and clears them on flag changes", () => {
  const { query } = harness();
  const key = ["scope", "collection", "overseer_plans", "", {}];
  query.setQueryData(key, { pages: [{ items: [plan()] }], pageParams: [""] });
  applyLiveEvent(query, "scope", "overseer", { kind: "overseer.planned" });
  expect(query.getQueryState(key)?.isInvalidated).toBe(true);
  applyLiveEvent(query, "scope", "overseer", { kind: "assistance.updated" });
  expect(query.getQueryData(key)).toBeUndefined();
});

it("retries a lost planning response with its original context and key after refreshed history", async () => {
  let turns = [plan()];
  const attempts: { body: any; key: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const body = requestBody(init);
      if (path.endsWith("/assistance/settings")) return json(settings);
      if (path.endsWith("/query"))
        return json({
          items: body.collection === "groups" ? [] : turns,
          order: "created_at_id_descending",
        });
      if (path.endsWith("/fleet/coordinate")) {
        attempts.push({
          body,
          key: (init?.headers as Record<string, string>)["Idempotency-Key"],
        });
        turns = [
          {
            ...plan("ovr_accepted"),
            prompt: body.prompt,
            previous_plan_id: body.previous_plan_id,
          },
          plan(),
        ];
        return attempts.length === 1
          ? json({ error: { message: "Response lost" } }, 503)
          : json(turns[0]);
      }
      return json([]);
    }),
  );
  const { Wrapper, query } = harness();
  render(<Overseer />, { wrapper: Wrapper });
  await screen.findByText("How is Scout doing?");
  fireEvent.change(
    screen.getByRole("textbox", { name: "Message Coordinator" }),
    {
      target: { value: "Follow up after loss" },
    },
  );
  fireEvent.click(screen.getByRole("button", { name: "Send to Coordinator" }));
  await screen.findByText("Response lost");
  await act(() => query.invalidateQueries());
  await screen.findByText("Follow up after loss");
  fireEvent.click(screen.getByRole("button", { name: "Send to Coordinator" }));
  await waitFor(() => expect(attempts).toHaveLength(2));
  expect(attempts[1]).toEqual(attempts[0]);
  expect(attempts[1].body.previous_plan_id).toBe("ovr_first");
});

it("keeps an explicitly new topic across navigation instead of attaching an old parent", async () => {
  const submitted: any[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const body = requestBody(init);
      if (path.endsWith("/assistance/settings")) return json(settings);
      if (path.endsWith("/query"))
        return json({
          items: body.collection === "groups" ? [] : [plan()],
          order: "created_at_id_descending",
        });
      if (path.endsWith("/fleet/coordinate")) {
        submitted.push(body);
        return json({ ...plan("new_topic"), prompt: body.prompt });
      }
      return json([]);
    }),
  );
  const first = harness();
  const view = render(<Overseer />, { wrapper: first.Wrapper });
  await screen.findByText("How is Scout doing?");
  fireEvent.click(screen.getByRole("button", { name: "New topic" }));
  fireEvent.change(
    screen.getByRole("textbox", { name: "New topic for Coordinator" }),
    { target: { value: "Independent topic" } },
  );
  view.unmount();
  const second = harness();
  render(<Overseer />, { wrapper: second.Wrapper });
  await waitFor(() =>
    expect(
      screen.getByRole("textbox", { name: "New topic for Coordinator" }),
    ).toHaveValue("Independent topic"),
  );
  fireEvent.click(screen.getByRole("button", { name: "Send to Coordinator" }));
  await waitFor(() =>
    expect(submitted).toEqual([{ prompt: "Independent topic" }]),
  );
});
it("expires a proposal locally without requiring a whole history refresh", async () => {
  const expiring = {
    ...plan(),
    operation: "question",
    requires_confirmation: true,
    body: "Check status",
    expires_at: new Date(Date.now() + 300).toISOString(),
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (path.endsWith("/assistance/settings")) return json(settings);
      if (path.endsWith("/query"))
        return json({
          items: requestBody(init).collection === "groups" ? [] : [expiring],
          order: "created_at_id_descending",
        });
      return json([]);
    }),
  );
  const { Wrapper } = harness();
  render(<Overseer />, { wrapper: Wrapper });
  const confirm = await screen.findByRole("button", {
    name: "Confirm exact instruction",
  });
  await waitFor(() => expect(confirm).toBeDisabled());
  await screen.findByText(/This proposal expired/);
});
