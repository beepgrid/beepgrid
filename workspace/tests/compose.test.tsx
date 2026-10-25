import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Compose } from "../src/Compose";
import { harness, agent, me, json, requestBody } from "./fixtures";
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  history.replaceState(null, "", "/agents");
});
const preview = {
  recipients: [
    { target: { kind: "agent", id: agent.id }, name: agent.name, routes: [] },
  ],
  count: 1,
  hidden_recipients: 0,
  token: "preview-fence",
  expires_at: "2099-01-01T00:00:00Z",
  sends: false,
};
it("requires recipient review and drops a review when content changes", async () => {
  const fetcher = vi.fn(async (path: string, init?: RequestInit) =>
    path.endsWith("/query")
      ? json({ items: [agent] })
      : path.endsWith("/dispatch-preview")
        ? json(preview)
        : json({ conversation_id: "c" }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(
    <Compose initial={[{ kind: "agent", id: agent.id }]} onClose={vi.fn()} />,
    { wrapper: Wrapper },
  );
  fireEvent.change(screen.getByRole("textbox", { name: /Message/ }), {
    target: { value: "First body" },
  });
  expect(
    screen.queryByRole("button", { name: "Confirm and send" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Review recipients" }));
  await screen.findByRole("button", { name: "Confirm and send" });
  expect(fetcher.mock.calls.some(([p]) => p.endsWith("/messages"))).toBe(false);
  fireEvent.change(screen.getByRole("textbox", { name: /Message/ }), {
    target: { value: "Changed body" },
  });
  expect(
    screen.queryByRole("button", { name: "Confirm and send" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Review recipients" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Confirm and send" }),
  );
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([p]) => p.endsWith("/messages"))).toBe(
      true,
    ),
  );
  const call = fetcher.mock.calls.find(([p]) => p.endsWith("/messages"))!;
  expect(requestBody(call[1])).toMatchObject({
    body: "Changed body",
    preview_token: "preview-fence",
  });
});
it("sends clarification in the existing conversation without an incompatible new-dispatch token", async () => {
  const fetcher = vi.fn(async (path: string, init?: RequestInit) =>
    path.endsWith("/capabilities")
      ? json({ operations: { message: true } })
      : path.endsWith("/conversations/c")
        ? json({ id: "c", participants: [me.person, agent] })
        : json({ conversation_id: "c" }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(
    <Compose
      initial={[{ kind: "agent", id: agent.id }]}
      conversation="c"
      onClose={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.change(screen.getByRole("textbox", { name: /Message/ }), {
    target: { value: "Please clarify" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Review recipients" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Confirm and send" }),
  );
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([p]) => p.endsWith("/messages"))).toBe(
      true,
    ),
  );
  const call = fetcher.mock.calls.find(([p]) => p.endsWith("/messages"))!;
  expect(requestBody(call[1])).toMatchObject({
    conversation_id: "c",
    body: "Please clarify",
  });
  expect(requestBody(call[1])).not.toHaveProperty("preview_token");
});
it("uses the batch contract and presents each independent receipt", async () => {
  const fetcher = vi.fn(async (path: string, init?: RequestInit) =>
    path.endsWith("/query")
      ? json({ items: [agent] })
      : path.endsWith("/dispatch-preview")
        ? json(preview)
        : json([
            {
              target: { kind: "agent", id: agent.id },
              request: { id: "req_one", conversation_id: "con_one" },
            },
            {
              target: { kind: "agent", id: "agt_two" },
              request: { id: "req_two", conversation_id: "con_two" },
            },
          ]),
  );
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(
    <Compose
      mode="question"
      multiple
      initial={[
        { kind: "agent", id: agent.id },
        { kind: "agent", id: "agt_two" },
      ]}
      onClose={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.change(screen.getByRole("textbox", { name: "Title" }), {
    target: { value: "Status?" },
  });
  fireEvent.change(screen.getByRole("textbox", { name: /Context/ }), {
    target: { value: "Please report" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Review recipients" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Confirm and send" }),
  );
  await screen.findByRole("heading", { name: "Dispatch accepted" });
  const call = fetcher.mock.calls.find(([p]) =>
    p.endsWith("/request-batches"),
  )!;
  const body = requestBody(call[1]);
  expect(body.targets).toHaveLength(2);
  expect(body.preview_token).toBe("preview-fence");
  expect(body.request).not.toHaveProperty("target");
  expect(body.request).not.toHaveProperty("conversation_id");
  expect(body.request).not.toHaveProperty("preview_token");
  expect(screen.getAllByRole("button", { name: "Open request" })).toHaveLength(
    2,
  );
});
