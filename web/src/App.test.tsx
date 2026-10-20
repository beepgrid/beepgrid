// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { App } from "./App";
import { requestLabel } from "./api";

const person = {
  id: "per_owner",
  kind: "person",
  name: "Alex Morgan",
  external_id: "alex",
  metadata: { department: "Finance" },
  disabled: false,
  created_at: "2026-10-20T08:00:00Z",
};
const agent = {
  id: "agt_scout",
  kind: "agent",
  name: "Scout",
  external_id: "research/scout",
  metadata: { project: "Research" },
  disabled: false,
  created_at: "2026-10-20T08:00:00Z",
};
let requests: Record<string, unknown>[],
  calls: { path: string; body: unknown }[];
beforeEach(() => {
  history.replaceState(null, "", "/");
  requests = [
    {
      id: "req_1",
      requester_id: agent.id,
      conversation_id: "con_1",
      kind: "question",
      title: "Which format?",
      body: "Choose an output format.",
      choices: ["Brief", "Detailed"],
      state: "open",
      version: 1,
      created_at: "2026-10-20T08:00:00Z",
      expires_at: "2026-11-20T08:00:00Z",
    },
    {
      id: "req_2",
      requester_id: agent.id,
      conversation_id: "con_2",
      kind: "question",
      title: "Which locale?",
      body: "Choose locale.",
      choices: ["English", "Hindi"],
      state: "open",
      version: 1,
      created_at: "2026-10-20T08:00:00Z",
      expires_at: "2026-11-20T08:00:00Z",
    },
  ];
  calls = [];
  vi.stubGlobal(
    "EventSource",
    class {
      onmessage: unknown;
      close() {}
    },
  );
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute("open", "");
  };
  Element.prototype.scrollIntoView = function () {};
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      const path = input.replace("/api/v1", "");
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ path, body });
      let value: unknown = {};
      if (path === "/me")
        value = {
          person,
          admin: true,
          company: "Acme",
          environment: "development",
        };
      else if (path === "/people")
        value = body ? { ...person, ...body } : [person];
      else if (path === "/agents") value = [agent];
      else if (path === "/requests") value = requests.map((x) => ({ ...x }));
      else if (path === "/agents/agt_scout/instances")
        value = [
          {
            id: "ins_1",
            agent_id: agent.id,
            presence: "connected",
            last_seen: "2026-10-20T08:00:00Z",
            capabilities: ["message"],
            progress: {},
          },
        ];
      else if (path === "/conversations/con_1")
        value = {
          id: "con_1",
          title: "Which format?",
          participants: [person, agent],
          messages: [],
        };
      else if (path === "/conversations")
        value = [
          {
            id: "con_1",
            title: "Which format?",
            created_at: "2026-10-20T08:00:00Z",
          },
        ];
      else if (path.endsWith("/responses")) {
        requests = requests.map((x) =>
          x.id === path.split("/")[2]
            ? { ...x, state: "resolved", response: body }
            : x,
        );
        value = requests[0];
      } else value = [];
      return { ok: true, status: 200, json: async () => value };
    }),
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
describe("employee workspace", () => {
  it("keeps people and agents in separate directories", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "Inbox", level: 1 });
    fireEvent.click(
      screen.getByRole("button", { name: "People" }),
    );
    expect(
      await screen.findByRole("heading", { name: "People", level: 1 }),
    ).toBeVisible();
    expect(screen.getAllByText("Alex Morgan").length).toBeGreaterThan(0);
    expect(screen.queryByText("research/scout")).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Agents" }),
    );
    expect(await screen.findByText("research/scout")).toBeVisible();
  });
  it("answers one request and focuses the remaining request", async () => {
    render(<App />);
    fireEvent.click(
      await screen.findByRole("button", { name: /Which format/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Brief" }));
    await waitFor(() =>
      expect(
        calls.some(
          (c) =>
            c.path === "/requests/req_1/responses" &&
            (c.body as { answer: string }).answer === "Brief",
        ),
      ).toBe(true),
    );
    expect(
      await screen.findByRole("heading", { name: "Which locale?", level: 2 }),
    ).toBeVisible();
    expect(requests.find((x) => x.id === "req_2")?.state).toBe("open");
  });
  it("clarification opens conversation without resolving a request", async () => {
    render(<App />);
    fireEvent.click(
      await screen.findByRole("button", { name: /Which format/ }),
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: /Ask for detail in the conversation/,
      }),
    );
    await screen.findByRole("textbox", { name: "Message" });
    expect(calls.some((c) => c.path.endsWith("/responses"))).toBe(false);
    expect(requests.every((x) => x.state === "open")).toBe(true);
  });
  it("manual person entry preserves department and human manager fields", async () => {
    render(<App />);
    await screen.findByRole("heading", { name: "Inbox", level: 1 });
    fireEvent.click(
      screen.getByRole("button", { name: "People" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Add person" }));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Taylor" },
    });
    fireEvent.change(screen.getByLabelText("Department"), {
      target: { value: "Engineering" },
    });
    fireEvent.change(screen.getByLabelText("Manager"), {
      target: { value: "per_owner" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(
        calls.some(
          (c) =>
            c.path === "/people" &&
            (c.body as { department?: string })?.department === "Engineering" &&
            (c.body as { manager_id: string }).manager_id === "per_owner",
        ),
      ).toBe(true),
    );
  });
  it("uses distinct terminal labels", () => {
    expect(
      requestLabel({
        state: "resolved",
        response: { outcome: "approved" },
      } as never),
    ).toBe("approved");
    expect(requestLabel({ state: "superseded" } as never)).toBe("superseded");
  });
});
