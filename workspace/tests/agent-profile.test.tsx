import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import Directory from "../src/pages/Directory";
import AgentProfile from "../src/pages/AgentProfile";
import { agent, harness, json, me, requestBody } from "./fixtures";

afterEach(() => {
  vi.unstubAllGlobals();
  history.replaceState(null, "", "/agents");
});

it("opens an exact deep link beyond the loaded directory, with role, runtime and communication context", async () => {
  history.replaceState(null, "", "/agents/agt_exact");
  const requests: Record<string, any>[] = [];
  const exact = {
    ...agent,
    id: "agt_exact",
    name: "Builder 7",
    metadata: { description: "Checkout API" },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const b = requestBody(init);
      if (path.endsWith("/capabilities"))
        return json({
          operations: { view: true, message: true },
          outcomes: [],
        });
      if (path.endsWith("/query")) {
        requests.push(b);
        let items: unknown[] = [];
        if (b.collection === "agents")
          items = b.filters?.id === exact.id ? [exact] : [agent];
        if (b.collection === "groups" && b.filters?.member_id)
          items = [
            {
              id: "grp_builders",
              name: "Builders",
              purpose: "agent_role",
              members: [exact.id],
            },
          ];
        if (b.collection === "instances")
          items = [
            {
              id: "ins_one",
              agent_id: exact.id,
              presence: "connected",
              last_seen: "2026-10-30T10:00:00Z",
            },
            {
              id: "ins_two",
              agent_id: exact.id,
              presence: "offline",
              last_seen: "2026-10-29T10:00:00Z",
            },
          ];
        if (b.collection === "conversations")
          items = [
            {
              id: "con_ledger",
              title: "Ledger lookup",
              participants: [
                exact,
                { ...agent, id: "agt_billing", name: "Billing 2" },
              ],
              last_preview: "The captures settled.",
              last_activity_at: "2026-10-30T10:00:00Z",
            },
          ];
        if (b.collection === "requests" && b.filters?.requester_id)
          items = [
            {
              id: "req_one",
              requester_id: exact.id,
              kind: "question",
              body: "Which settlement batch contains order 1042?",
              state: "open",
              conversation_id: "con_ledger",
            },
          ];
        return json({ items });
      }
      return json({ agents: [], deadlocks: [] });
    }),
  );
  const { Wrapper } = harness();
  render(<Directory section="agents" />, { wrapper: Wrapper });
  await screen.findByRole("heading", { name: "Builder 7" });
  expect(await screen.findByText("Builders")).toBeVisible();
  expect(
    await screen.findByRole("button", { name: /Which settlement batch/ }),
  ).toBeVisible();
  expect(
    screen.getByRole("button", { name: /Billing 2 The captures settled/ }),
  ).toBeVisible();
  expect(
    screen.getByRole("button", { name: "Message connection 2" }),
  ).toBeVisible();
  expect(
    requests.some(
      (b) =>
        b.collection === "conversations" &&
        b.filters.participant_id === exact.id,
    ),
  ).toBe(true);
  expect(
    requests.some(
      (b) => b.collection === "groups" && b.filters.member_id === exact.id,
    ),
  ).toBe(true);
  fireEvent.click(
    screen.getByRole("button", { name: /Which settlement batch/ }),
  );
  expect(location.pathname).toBe("/conversations/con_ledger");
});

it("conceals a previously loaded agent profile as soon as authority is lost", async () => {
  let denied = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const b = requestBody(init);
      if (path.endsWith("/query")) {
        if (b.collection === "agents" && denied)
          return json(
            { error: { code: "NOT_FOUND", message: "Agent unavailable" } },
            404,
          );
        return json({ items: b.collection === "agents" ? [agent] : [] });
      }
      return json({ operations: { view: true }, outcomes: [] });
    }),
  );
  const { Wrapper, query } = harness();
  render(<AgentProfile id={agent.id} onConnect={() => {}} />, {
    wrapper: Wrapper,
  });
  await screen.findByRole("heading", { name: agent.name });
  denied = true;
  await act(() => query.invalidateQueries());
  await screen.findByRole("alert");
  expect(
    screen.queryByRole("heading", { name: agent.name }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("tab", { name: "Conversations" }),
  ).not.toBeInTheDocument();
});

it("uses role and current-presence filters without confusing roles with runtime replicas", async () => {
  const queries: any[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const b = requestBody(init);
      if (path.endsWith("/query")) {
        queries.push(b);
        return json({
          items:
            b.collection === "groups"
              ? [{ id: "grp_one", name: "Builders" }]
              : [],
        });
      }
      return json({ operations: {}, agents: [], deadlocks: [] });
    }),
  );
  const { Wrapper } = harness();
  render(<Directory section="agents" />, { wrapper: Wrapper });
  await screen.findByRole("option", { name: "Builders" });
  fireEvent.change(
    screen.getByRole("combobox", { name: "Filter by agent role" }),
    { target: { value: "grp_one" } },
  );
  fireEvent.change(
    screen.getByRole("combobox", { name: "Filter by connection" }),
    { target: { value: "offline" } },
  );
  await waitFor(() =>
    expect(
      queries.some(
        (b) =>
          b.collection === "agents" &&
          b.filters.group_id === "grp_one" &&
          b.filters.presence === "offline",
      ),
    ).toBe(true),
  );
});
