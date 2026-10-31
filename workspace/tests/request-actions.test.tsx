import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { RequestCard } from "../src/RequestCard";
import Settings from "../src/pages/Settings";
import type { Identity, Request } from "../src/contracts";
import { agent, harness, json, me, requestBody } from "./fixtures";

const question = {
  id: "req_one",
  requester_id: agent.id,
  conversation_id: "con_one",
  kind: "question",
  body: "What is the checkout freeze time?",
  title: "Checkout freeze",
  state: "open",
  version: 7,
  choices: [],
  shared_key: "checkout/freeze/v2",
  created_at: "2026-10-30T00:00:00Z",
  expires_at: "2126-10-30T00:00:00Z",
} as Request;
afterEach(() => {
  vi.unstubAllGlobals();
  delete me.can_publish_answers;
  history.replaceState(null, "", "/agents");
});

it("separates Coordinator context from the exact request and preserves other titles", () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => json({ operations: {} })),
  );
  const { Wrapper } = harness();
  const { rerender } = render(
    <RequestCard
      request={{ ...question, title: "Coordinator: Review release readiness" }}
    />,
    { wrapper: Wrapper },
  );
  expect(screen.getByLabelText("Coordinator context")).toHaveTextContent(
    "Review release readiness",
  );
  expect(screen.getByText(question.body)).toBeVisible();
  rerender(<RequestCard request={{ ...question, title: "Other context" }} />);
  expect(
    screen.queryByLabelText("Coordinator context"),
  ).not.toBeInTheDocument();
  expect(screen.getByText("Other context")).toBeVisible();
});

it("confirms cancellation with the observed version and keeps history on a stale decision", async () => {
  const fetcher = vi.fn(async (path: string, _init?: RequestInit) =>
    path.endsWith("/capabilities")
      ? json({ operations: { cancel: true } })
      : json(
          {
            error: {
              code: "CONFLICT",
              message: "This request changed. Reload before deciding.",
            },
          },
          409,
        ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(
    <RequestCard
      request={{ ...question, kind: "approval" }}
      sender={agent as Identity}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "Cancel approval" }),
  );
  expect(fetcher.mock.calls.some(([path]) => path.endsWith("/cancel"))).toBe(
    false,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Cancel pending request" }),
  );
  await screen.findByRole("alert");
  const call = fetcher.mock.calls.find(([path]) =>
    path.endsWith("/requests/req_one/cancel"),
  );
  expect(call).toBeDefined();
  expect(requestBody(call![1])).toEqual({ version: 7 });
  expect(screen.getByText(question.body)).toBeVisible();
});

it("shares only after an explicit audience and answer are chosen, using the atomic server operation", async () => {
  me.can_publish_answers = true;
  const second = { ...agent, id: "agt_second", name: "Reviewer 2" };
  const fetcher = vi.fn(async (path: string, init?: RequestInit) => {
    if (path.endsWith("/capabilities"))
      return json({ operations: { respond: true } });
    if (path.endsWith("/query"))
      return json({
        items: requestBody(init).collection === "agents" ? [agent, second] : [],
      });
    return json({ ...question, state: "resolved" });
  });
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(<RequestCard request={question} sender={agent as Identity} />, {
    wrapper: Wrapper,
  });
  fireEvent.click(
    await screen.findByRole("button", { name: "Answer and share" }),
  );
  fireEvent.change(screen.getByRole("textbox", { name: "Answer" }), {
    target: { value: "The API freezes at 14:00 UTC." },
  });
  fireEvent.click(await screen.findByRole("checkbox", { name: /Reviewer 2/ }));
  expect(screen.getByRole("checkbox", { name: /Reviewer 2/ })).toBeChecked();
  expect(
    fetcher.mock.calls.some(([path]) => path.endsWith("/answer-and-share")),
  ).toBe(false);
  fireEvent.click(
    screen.getByRole("button", { name: "Send answer and share" }),
  );
  await waitFor(() =>
    expect(
      fetcher.mock.calls.some(([path]) => path.endsWith("/answer-and-share")),
    ).toBe(true),
  );
  const call = fetcher.mock.calls.find(([path]) =>
    path.endsWith("/answer-and-share"),
  )!;
  expect(requestBody(call[1])).toEqual({
    version: 7,
    outcome: "answered",
    answer: "The API freezes at 14:00 UTC.",
    audience: [agent.id, second.id],
  });
  expect(
    fetcher.mock.calls.some(
      ([path]) => path.endsWith("/responses") || path.endsWith("/answers"),
    ),
  ).toBe(false);
});

it("never offers informational sharing for an approval and labels invalidated provenance", async () => {
  me.can_publish_answers = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => json({ operations: { respond: true } })),
  );
  const { Wrapper } = harness();
  const { rerender } = render(
    <RequestCard request={{ ...question, kind: "approval" }} />,
    { wrapper: Wrapper },
  );
  await screen.findByRole("button", { name: "Approve" });
  expect(
    screen.queryByRole("button", { name: "Answer and share" }),
  ).not.toBeInTheDocument();
  rerender(
    <RequestCard
      request={{
        ...question,
        state: "resolved",
        response: {
          outcome: "answered",
          answer: "14:00 UTC",
          source_answer_id: "ans_saved",
          source_invalidated: true,
        },
      }}
    />,
  );
  expect(screen.getByText("Saved answer invalidated")).toBeVisible();
});

it("makes answer management reachable for a delegated publisher without exposing admin tools", async () => {
  me.can_publish_answers = true;
  history.replaceState(null, "", "/settings/account");
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => json({ workspace: {} })),
  );
  const { Wrapper } = harness();
  render(<Settings />, { wrapper: Wrapper });
  expect(
    await screen.findByRole("button", { name: "Published answers" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "Access tokens" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Activity log" }),
  ).not.toBeInTheDocument();
});

it("edits an own pending message through one atomic replacement and retains a failed draft", async () => {
  const fetcher = vi.fn(async (path: string, _init?: RequestInit) =>
    path.endsWith("/capabilities")
      ? json({ operations: { replace: true } })
      : json(
          {
            error: {
              code: "CONFLICT",
              message:
                "This message received a response. Refresh before replacing it.",
            },
          },
          409,
        ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(
    <RequestCard
      request={{ ...question, kind: "approval" }}
      sender={agent as Identity}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "Edit and resend" }),
  );
  expect(fetcher.mock.calls.some(([p]) => p.endsWith("/replace"))).toBe(false);
  expect(
    screen.getByText("A fresh approval is required for the updated message."),
  ).toBeVisible();
  fireEvent.change(screen.getByRole("textbox", { name: "Message" }), {
    target: { value: "Can staging restart after the demo?" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send replacement" }));
  await screen.findByRole("alert");
  expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue(
    "Can staging restart after the demo?",
  );
  const call = fetcher.mock.calls.find(([p]) =>
    p.endsWith("/requests/req_one/replace"),
  )!;
  expect(requestBody(call[1])).toEqual({
    version: 7,
    body: "Can staging restart after the demo?",
    mentions: [],
  });
  expect(
    fetcher.mock.calls.some(
      ([p]) => p.endsWith("/cancel") || p.endsWith("/requests"),
    ),
  ).toBe(false);
});
