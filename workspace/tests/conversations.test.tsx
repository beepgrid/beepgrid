import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import Conversations from "../src/pages/Conversations";
import { agent, harness, json, me, requestBody } from "./fixtures";

it("shows only two ticks on outgoing messages acknowledged by every Agent in the chat", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  const second = { ...agent, id: "agt_second", name: "Reviewer" };
  const messages = [
    {
      id: "msg_acked",
      sender_id: me.person.id,
      body: "Acknowledged message",
      received_by: [agent.id, second.id],
    },
    {
      id: "msg_partial",
      sender_id: me.person.id,
      body: "One Agent still pending",
      received_by: [agent.id],
    },
    { id: "msg_queued", sender_id: me.person.id, body: "Queued message" },
    {
      id: "msg_incoming",
      sender_id: agent.id,
      body: "Incoming message",
      received_by: [second.id],
    },
  ].map((message, index) => ({
    ...message,
    conversation_id: "con_one",
    created_at: `2026-11-01T00:00:0${index}Z`,
  }));
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (path.endsWith("/capabilities"))
        return json({ operations: { view: true, message: true } });
      if (path.endsWith("/query"))
        return json({
          items: requestBody(init).collection === "messages" ? messages : [],
        });
      return json({
        id: "con_one",
        participants: [me.person, agent, second],
        messages,
      });
    }),
  );
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  const ticks = await screen.findByRole("img", {
    name: "Agent runtime acknowledged receipt",
  });
  expect(ticks.querySelectorAll("svg")).toHaveLength(1);
  expect(ticks.textContent).toBe("");
  expect(ticks.closest("article")).toHaveTextContent("Acknowledged message");
  expect(
    screen.getAllByRole("img", { name: "Agent runtime acknowledged receipt" }),
  ).toHaveLength(1);
});

afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  history.replaceState(null, "", "/agents");
});

function question(id = "req_question", time = "2026-11-01T00:00:00Z") {
  return {
    id,
    conversation_id: "con_one",
    requester_id: agent.id,
    kind: "question",
    title: "Freeze time",
    body: "When should the API freeze?",
    state: "open",
    version: 7,
    choices: [],
    created_at: time,
  };
}
function conversationTransport(
  requests: any[],
  message = true,
  respond = true,
) {
  const writes: { path: string; body: any }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const body = requestBody(init);
      if (path.endsWith("/capabilities"))
        return json({
          operations: body.request_id ? { respond } : { view: true, message },
        });
      if (path.endsWith("/query"))
        return json({
          items:
            body.collection === "requests"
              ? requests.filter(
                  (r) => body.filters?.state !== "open" || r.state === "open",
                )
              : [],
        });
      if (path.endsWith("/responses")) {
        writes.push({ path, body });
        const request = requests.find((r) => path.includes(r.id));
        Object.assign(request, {
          state: "resolved",
          resolved_by: me.person.id,
          resolved_at: "2026-11-01T00:01:00Z",
          response: { outcome: body.outcome, answer: body.answer },
        });
        return json(request);
      }
      if (path.endsWith("/messages")) {
        writes.push({ path, body });
        return json({ conversation_id: "con_one" });
      }
      return json({
        id: "con_one",
        participants: [agent, me.person],
        messages: [],
      });
    }),
  );
  return writes;
}
it("keeps chat corrections separate from explicit question answers", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  const requests = [question()];
  const writes = conversationTransport(requests);
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  const answer = await screen.findByRole("textbox", {
    name: "Answer question",
  });
  const message = screen.getByRole("textbox", { name: "Message" });
  fireEvent.change(answer, { target: { value: "At 14:00 UTC." } });
  fireEvent.change(message, {
    target: { value: "Actually, use Marcus instead of Johnson." },
  });
  fireEvent.keyDown(message, { key: "Enter" });
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0].path).toBe("/api/v1/messages");
  expect(requests[0].state).toBe("open");
  expect(answer).toHaveValue("At 14:00 UTC.");
  fireEvent.click(screen.getByRole("button", { name: "Answer" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]).toEqual({
    path: "/api/v1/requests/req_question/responses",
    body: { version: 7, outcome: "answered", answer: "At 14:00 UTC." },
  });
  await waitFor(() =>
    expect(
      screen.queryByRole("region", { name: "Pending questions" }),
    ).not.toBeInTheDocument(),
  );
  expect(screen.getByRole("textbox", { name: "Message" })).toBeVisible();
});
it("allows a scoped answer without granting ordinary message access", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  const writes = conversationTransport([question()], false);
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  const answer = await screen.findByRole("textbox", {
    name: "Answer question",
  });
  expect(
    screen.queryByRole("textbox", { name: "Message" }),
  ).not.toBeInTheDocument();
  fireEvent.change(answer, { target: { value: "At noon." } });
  fireEvent.keyDown(answer, { key: "Enter", shiftKey: true });
  expect(writes).toHaveLength(0);
  fireEvent.keyDown(answer, { key: "Enter", isComposing: true });
  expect(writes).toHaveLength(0);
  fireEvent.keyDown(answer, { key: "Enter" });
  await waitFor(() => expect(writes[0]?.body.answer).toBe("At noon."));
});

it("labels a Coordinator request as outgoing and never offers an administrator a form to answer their own question", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (path.endsWith("/capabilities"))
        return json({
          operations: { view: true, message: true, respond: true },
        });
      if (path.endsWith("/query"))
        return json({
          items:
            requestBody(init).collection === "requests"
              ? [
                  {
                    id: "req_outgoing",
                    conversation_id: "con_one",
                    requester_id: me.person.id,
                    kind: "question",
                    title: "Coordinator: Prepare the rollout",
                    body: "Check the fallback.",
                    state: "open",
                    choices: [],
                    version: 1,
                    created_at: "2026-11-01T00:00:00Z",
                  },
                ]
              : [],
        });
      return json({
        id: "con_one",
        title: "Rollout",
        participants: [agent, me.person],
        messages: [],
      });
    }),
  );
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  await screen.findByRole("img", { name: "You via Coordinator" });
  await screen.findByText("Awaiting reply");
  expect(screen.getByText("To Research assistant")).toBeVisible();
  expect(
    screen.queryByRole("textbox", { name: "Reply" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Send reply" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Message" })).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "View Agent" }));
  expect(location.pathname).toBe("/agents/" + agent.id);
});

it("shows the latest question first and keeps separate drafts through navigation and collapse", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  const requests = [
    question("older"),
    {
      ...question("latest", "2026-11-01T00:01:00Z"),
      body: "Who should receive the report?",
    },
  ];
  conversationTransport(requests);
  const { Wrapper, query } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  let answer = await screen.findByRole("textbox", { name: "Answer question" });
  const dock = screen.getByRole("region", { name: "Pending questions" });
  expect(
    within(dock).getByText("Who should receive the report?"),
  ).toBeVisible();
  fireEvent.change(answer, { target: { value: "Marcus" } });
  fireEvent.click(screen.getByRole("button", { name: "Minimize questions" }));
  expect(
    screen.queryByRole("textbox", { name: "Answer question" }),
  ).not.toBeInTheDocument();
  expect(
    within(dock).getByRole("button", {
      name: "Who should receive the report?",
    }),
  ).toBeVisible();
  expect(screen.getByRole("textbox", { name: "Message" })).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Expand questions" }));
  expect(screen.getByRole("textbox", { name: "Answer question" })).toHaveValue(
    "Marcus",
  );
  fireEvent.click(screen.getByRole("button", { name: "Next question" }));
  answer = screen.getByRole("textbox", { name: "Answer question" });
  expect(answer).toHaveValue("");
  fireEvent.change(answer, { target: { value: "14:00" } });
  requests.push({
    ...question("arriving", "2026-11-01T00:02:00Z"),
    body: "A new live question",
  });
  await act(() => query.invalidateQueries());
  expect(screen.getByRole("textbox", { name: "Answer question" })).toHaveValue(
    "14:00",
  );
  fireEvent.click(screen.getByRole("button", { name: "Previous question" }));
  expect(screen.getByRole("textbox", { name: "Answer question" })).toHaveValue(
    "Marcus",
  );
});
it("requires an explicit choice and leaves all options unselected initially", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  const writes = conversationTransport([
    { ...question(), body: "Which region?", choices: ["Europe", "India"] },
  ]);
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  const choice = await screen.findByRole("radio", { name: "India" });
  expect(choice).not.toBeChecked();
  expect(screen.getByRole("radio", { name: "Europe" })).not.toBeChecked();
  expect(screen.getByRole("button", { name: "Answer" })).toBeDisabled();
  fireEvent.click(choice);
  fireEvent.click(screen.getByRole("button", { name: "Answer" }));
  await waitFor(() => expect(writes[0]?.body.answer).toBe("India"));
});
it("keeps ordinary messaging available when answering is denied", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  const writes = conversationTransport([question()], true, false);
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  await screen.findByText("You can no longer answer this question.");
  expect(
    screen.queryByRole("textbox", { name: "Answer question" }),
  ).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole("textbox", { name: "Message" }), {
    target: { value: "An independent follow-up" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(writes[0]?.path).toBe("/api/v1/messages"));
});
it("loads older pending questions through next without disabling chat", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const b = requestBody(init);
      if (path.endsWith("/capabilities"))
        return json({
          operations: { view: true, message: true, respond: true },
        });
      if (path.endsWith("/query"))
        return json(
          b.collection === "requests" && b.filters.state === "open"
            ? b.cursor
              ? {
                  items: [
                    { ...question("older"), body: "Older pending question" },
                  ],
                }
              : {
                  items: [question("latest", "2026-11-01T00:01:00Z")],
                  next_cursor: "more",
                }
            : { items: [] },
        );
      return json({
        id: "con_one",
        participants: [agent, me.person],
        messages: [],
      });
    }),
  );
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  await screen.findByRole("textbox", { name: "Answer question" });
  expect(screen.getByRole("textbox", { name: "Message" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Next question" }));
  await screen.findByText("Older pending question");
});

it("retains a draft through a temporary read outage but clears it after authoritative contact loss", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  let failure = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path.endsWith("/messages"))
        return json(
          { error: { code: "UNAVAILABLE", message: "Send result is unknown" } },
          503,
        );
      if (path.endsWith("/query"))
        return json({ items: [], order: "id_ascending" });
      if (failure)
        return json(
          {
            error: {
              code: failure === 404 ? "NOT_FOUND" : "UNAVAILABLE",
              message: "Temporarily unavailable",
            },
          },
          failure,
        );
      if (path.endsWith("/capabilities"))
        return json({
          operations: { view: true, message: true },
          outcomes: [],
        });
      return json({
        id: "con_one",
        title: "Private context",
        participants: [agent, me.person],
        messages: [],
      });
    }),
  );
  const { Wrapper, query } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  const composer = await screen.findByRole("textbox", { name: "Message" });
  fireEvent.change(composer, { target: { value: "Keep my unsent work" } });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await screen.findByRole("alert");
  expect(
    Object.keys(sessionStorage).some(
      (k) =>
        k.includes(":operation:") &&
        sessionStorage.getItem(k)?.includes("Keep my unsent work"),
    ),
  ).toBe(true);
  failure = 503;
  await act(() => query.invalidateQueries());
  await screen.findByRole("alert");
  expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue(
    "Keep my unsent work",
  );
  failure = 0;
  await act(() => query.invalidateQueries());
  await waitFor(() =>
    expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue(
      "Keep my unsent work",
    ),
  );
  failure = 404;
  await act(() => query.invalidateQueries());
  await screen.findByRole("alert");
  await waitFor(() =>
    expect(
      Object.keys(sessionStorage).some((k) =>
        sessionStorage.getItem(k)?.includes("Keep my unsent work"),
      ),
    ).toBe(false),
  );
  failure = 0;
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  await waitFor(() =>
    expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue(""),
  );
});

it("keeps the latest reply visible after access loads and the composer changes size, without moving someone reading history", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  let allow: (value: Response) => void;
  const access = new Promise<Response>((resolve) => {
    allow = resolve;
  });
  const resized: (() => void)[] = [];
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(callback: () => void) {
        resized.push(callback);
      }
      observe() {}
      disconnect() {}
    },
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path.endsWith("/query")) return json({ items: [] });
      if (path.endsWith("/capabilities")) return access;
      return json({
        id: "con_one",
        title: "Release",
        participants: [agent, me.person],
        messages: [
          {
            id: "msg_one",
            sender_id: agent.id,
            body: "The migration is ready.",
            created_at: "2026-10-30T10:00:00Z",
          },
        ],
      });
    }),
  );
  const { Wrapper } = harness();
  const { container } = render(<Conversations />, { wrapper: Wrapper });
  await screen.findByText("The migration is ready.");
  const pane = container.querySelector<HTMLDivElement>(".messages")!;
  Object.defineProperties(pane, {
    scrollHeight: { value: 1200, configurable: true },
    clientHeight: { value: 400, configurable: true },
  });
  await act(async () => {
    allow!(
      json({
        operations: { view: true, message: true, ask: true },
        outcomes: [],
      }),
    );
  });
  await screen.findByRole("textbox", { name: "Message" });
  expect(pane.scrollTop).toBe(1200);
  pane.scrollTop = 800;
  fireEvent.scroll(pane);
  Object.defineProperty(pane, "clientHeight", {
    value: 250,
    configurable: true,
  });
  act(() => resized.forEach((cb) => cb()));
  expect(pane.scrollTop).toBe(1200);
  pane.scrollTop = 120;
  fireEvent.scroll(pane);
  Object.defineProperty(pane, "scrollHeight", {
    value: 1500,
    configurable: true,
  });
  act(() => resized.forEach((cb) => cb()));
  expect(pane.scrollTop).toBe(120);
});

it("keeps a failed answer bound to its question and never falls back to chat", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  const requests = [question()];
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const body = requestBody(init);
      if (path.endsWith("/capabilities"))
        return json({
          operations: { view: true, message: true, respond: true },
        });
      if (path.endsWith("/query"))
        return json({ items: body.collection === "requests" ? requests : [] });
      if (path.endsWith("/responses")) {
        writes.push(path);
        return json(
          {
            error: {
              code: "CONFLICT",
              message: "The request version changed.",
            },
          },
          409,
        );
      }
      if (path.endsWith("/messages")) {
        writes.push(path);
        return json({ conversation_id: "con_one" });
      }
      return json({
        id: "con_one",
        participants: [agent, me.person],
        messages: [],
      });
    }),
  );
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  const answer = await screen.findByRole("textbox", {
    name: "Answer question",
  });
  fireEvent.change(answer, { target: { value: "At 14:00 UTC." } });
  fireEvent.change(screen.getByRole("textbox", { name: "Message" }), {
    target: { value: "An unsent correction" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Answer" }));
  await screen.findByRole("alert");
  expect(screen.getByRole("textbox", { name: "Answer question" })).toHaveValue(
    "At 14:00 UTC.",
  );
  expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue(
    "An unsent correction",
  );
  expect(requests[0].state).toBe("open");
  expect(writes).toEqual(["/api/v1/requests/req_question/responses"]);
});

it("docks approvals alongside questions, preserves bound confirmation and jumps to the dated request", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  const approval = {
    ...question("req_approval", "2026-11-01T00:02:00Z"),
    kind: "approval",
    body: "May I update the sandbox report?",
    action: {
      name: "write_report",
      executor_id: agent.id,
      resource: "sandbox/report",
      resource_version: "v1",
      operation_key: "review-one",
      arguments: {},
    },
  };
  const writes = conversationTransport([question(), approval]);
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  const dock = await screen.findByRole("region", { name: "Pending questions" });
  await within(dock).findByRole("button", { name: "Approve" });
  expect(screen.getAllByRole("button", { name: "Approve" })).toHaveLength(1);
  const card = document.getElementById("request-req_approval")!;
  const scroll = vi.fn();
  card.scrollIntoView = scroll;
  expect(card.querySelector("time")).toHaveAttribute(
    "datetime",
    approval.created_at,
  );
  fireEvent.click(within(dock).getByRole("button", { name: approval.body }));
  await waitFor(() => expect(scroll).toHaveBeenCalled());
  expect(card).toHaveFocus();
  expect(
    within(dock).getByRole("button", { name: "Minimize questions" }),
  ).toHaveAttribute("aria-expanded", "true");
  fireEvent.click(
    within(dock).getByRole("button", { name: "Ask for context" }),
  );
  expect(screen.getByRole("textbox", { name: "Message" })).toHaveFocus();
  fireEvent.click(within(dock).getByRole("button", { name: "Approve" }));
  await within(dock).findByRole("button", { name: "Confirm approval" });
  expect(writes).toHaveLength(0);
  fireEvent.click(
    within(dock).getByRole("button", { name: "Confirm approval" }),
  );
  await waitFor(() =>
    expect(writes[0]).toEqual({
      path: "/api/v1/requests/req_approval/responses",
      body: { version: 7, outcome: "approved" },
    }),
  );
  await screen.findByRole("textbox", { name: "Answer question" });
});

it("shows the actual requester and icon in Needs attention", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const b = requestBody(init);
      if (path.endsWith("/capabilities"))
        return json({
          operations: { view: true, message: true, respond: true },
        });
      if (path.endsWith("/query"))
        return json({
          items:
            b.collection === "requests"
              ? [question()]
              : b.collection === "conversations"
                ? [
                    {
                      id: "con_one",
                      title: "Review",
                      participants: [agent, me.person],
                      created_at: question().created_at,
                    },
                  ]
                : b.collection === "agents"
                  ? [agent]
                  : [],
        });
      return json({
        id: "con_one",
        participants: [agent, me.person],
        messages: [],
      });
    }),
  );
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  fireEvent.click(screen.getByRole("button", { name: "Needs attention" }));
  const row = await screen.findByRole("button", {
    name: /Research assistant.*When should the API freeze/,
  });
  expect(row.querySelector(".identity-agent")).not.toBeNull();
  expect(within(row).getByText("question")).toBeVisible();
});

it("loads the request's older history page before jumping from the panel", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  const old = question("req_old");
  const cursors: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const b = requestBody(init);
      if (path.endsWith("/capabilities"))
        return json({
          operations: { view: true, message: true, respond: true },
        });
      if (path.endsWith("/query")) {
        if (b.collection !== "requests") return json({ items: [] });
        if (b.filters.state === "open")
          return json({ items: b.filters.kind === "question" ? [old] : [] });
        cursors.push(b.cursor);
        return json(
          b.cursor
            ? { items: [old] }
            : { items: [], next_cursor: "older-request-page" },
        );
      }
      return json({
        id: "con_one",
        participants: [agent, me.person],
        messages: [],
      });
    }),
  );
  const { Wrapper } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  const dock = await screen.findByRole("region", { name: "Pending questions" });
  fireEvent.click(within(dock).getByRole("button", { name: old.body }));
  await waitFor(() =>
    expect(document.getElementById("request-req_old")).toHaveFocus(),
  );
  expect(cursors).toContain("older-request-page");
});
