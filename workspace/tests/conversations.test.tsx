import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import Conversations from "../src/pages/Conversations";
import { agent, harness, json, me, requestBody } from "./fixtures";

afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  history.replaceState(null, "", "/agents");
});

it("answers an incoming request through one composer with scoped reply authority, without broad Message permission", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  let request: any = {
    id: "req_question",
    conversation_id: "con_one",
    requester_id: agent.id,
    kind: "question",
    title: "Freeze time",
    body: "When should the API freeze?",
    state: "open",
    version: 7,
    choices: [],
    created_at: "2026-11-01T00:00:00Z",
  };
  const writes: { path: string; body: any }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const body = requestBody(init);
      if (path.endsWith("/capabilities"))
        return json({
          operations: body.request_id
            ? { respond: true }
            : { view: true, message: false },
        });
      if (path.endsWith("/query"))
        return json({ items: body.collection === "requests" ? [request] : [] });
      if (path.endsWith("/responses")) {
        writes.push({ path, body });
        request = {
          ...request,
          state: "resolved",
          version: 8,
          resolved_by: me.person.id,
          resolved_at: "2026-11-01T00:01:00Z",
          response: { outcome: "answered", answer: body.answer },
        };
        return json(request);
      }
      return json({
        id: "con_one",
        title: "Freeze",
        participants: [agent, me.person],
        messages: [],
      });
    }),
  );
  const { Wrapper, query } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  const editor = await screen.findByRole("textbox", {
    name: "Reply",
  });
  expect(screen.getAllByRole("textbox")).toHaveLength(1);
  expect(
    screen.queryByRole("button", { name: "Attach files" }),
  ).not.toBeInTheDocument();
  fireEvent.change(editor, { target: { value: "At 14:00 UTC." } });
  await act(() => query.invalidateQueries());
  expect(
    Object.keys(sessionStorage).some((k) =>
      sessionStorage.getItem(k)?.includes("At 14:00 UTC."),
    ),
  ).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "Send reply" }));
  await screen.findByText("At 14:00 UTC.");
  expect(writes).toEqual([
    {
      path: "/api/v1/requests/req_question/responses",
      body: { version: 7, outcome: "answered", answer: "At 14:00 UTC." },
    },
  ]);
  expect(
    screen.queryByRole("textbox", { name: "Reply" }),
  ).not.toBeInTheDocument();
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
  await screen.findByText("You via Coordinator");
  await screen.findByText("Waiting for Research assistant");
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
