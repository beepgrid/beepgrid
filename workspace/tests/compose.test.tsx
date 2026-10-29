import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Compose } from "../src/Compose";
import { harness, agent, json, requestBody } from "./fixtures";
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  history.replaceState(null, "", "/agents");
});
function transport() {
  const fn = vi.fn(async (path: string, init?: RequestInit) =>
    path.endsWith("/query")
      ? json({
          items: requestBody(init).collection === "agents" ? [agent] : [],
        })
      : json({ conversation_id: "con_sent" }),
  );
  vi.stubGlobal("fetch", fn);
  return fn;
}
it("starts with recipients and one message, without protocol fields or a mandatory review", async () => {
  const fn = transport(),
    { Wrapper } = harness();
  render(
    <Compose initial={[{ kind: "agent", id: agent.id }]} onClose={vi.fn()} />,
    { wrapper: Wrapper },
  );
  expect(
    screen.queryByRole("textbox", { name: "Title" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("combobox", { name: "Operation" }),
  ).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole("textbox", { name: "Message" }), {
    target: { value: "Keep the migration below ten jobs." },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() =>
    expect(fn.mock.calls.some(([p]) => p.endsWith("/messages"))).toBe(true),
  );
  const payload = requestBody(
    fn.mock.calls.find(([p]) => p.endsWith("/messages"))![1],
  );
  expect(payload).toMatchObject({
    body: "Keep the migration below ten jobs.",
    target: { kind: "agent", id: agent.id },
  });
  expect(payload).not.toHaveProperty("title");
  expect(fn.mock.calls.some(([p]) => p.endsWith("/dispatch-preview"))).toBe(
    false,
  );
});
it("adds a communication approval only when requested", async () => {
  const fn = transport(),
    { Wrapper } = harness();
  render(
    <Compose initial={[{ kind: "agent", id: agent.id }]} onClose={vi.fn()} />,
    { wrapper: Wrapper },
  );
  expect(screen.getByText("Request approval")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Request approval" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Message" }), {
    target: { value: "Can we restart staging now?" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() =>
    expect(fn.mock.calls.some(([p]) => p.endsWith("/requests"))).toBe(true),
  );
  const payload = requestBody(
    fn.mock.calls.find(([p]) => p.endsWith("/requests"))![1],
  );
  expect(payload).toMatchObject({
    kind: "approval",
    body: "Can we restart staging now?",
  });
  expect(payload).not.toHaveProperty("action");
  expect(payload).not.toHaveProperty("expires_at");
});
it("uses visual answer choices and keeps them out of ordinary messages", async () => {
  const fn = transport(),
    { Wrapper } = harness();
  render(
    <Compose initial={[{ kind: "agent", id: agent.id }]} onClose={vi.fn()} />,
    { wrapper: Wrapper },
  );
  fireEvent.click(screen.getByRole("button", { name: "Add answer choices" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Message" }), {
    target: { value: "Which environment?" },
  });
  fireEvent.change(screen.getByRole("textbox", { name: "Choice 1" }), {
    target: { value: "Staging" },
  });
  fireEvent.change(screen.getByRole("textbox", { name: "Choice 2" }), {
    target: { value: "Preview" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() =>
    expect(fn.mock.calls.some(([p]) => p.endsWith("/requests"))).toBe(true),
  );
  expect(
    requestBody(fn.mock.calls.find(([p]) => p.endsWith("/requests"))![1])
      .choices,
  ).toEqual(["Staging", "Preview"]);
});
it("creates shared history only after choosing group chat", async () => {
  const fn = transport(),
    { Wrapper } = harness();
  render(
    <Compose
      initial={[
        { kind: "agent", id: agent.id },
        { kind: "agent", id: "agt_two" },
      ]}
      onClose={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.click(screen.getByRole("button", { name: "Create group chat" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Message" }), {
    target: { value: "@DevOps keep those jobs below ten." },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() =>
    expect(fn.mock.calls.some(([p]) => p.endsWith("/conversations"))).toBe(
      true,
    ),
  );
  expect(
    requestBody(fn.mock.calls.find(([p]) => p.endsWith("/conversations"))![1])
      .targets,
  ).toHaveLength(2);
  expect(fn.mock.calls.some(([p]) => p.endsWith("/message-batches"))).toBe(
    false,
  );
});
it("addresses a selected participant in the first shared message without inviting a different identity", async () => {
  const devops = { ...agent, id: "agt_devops", name: "DevOps 2" };
  const fn = vi.fn(async (path: string, init?: RequestInit) =>
    path.endsWith("/query")
      ? json({
          items:
            requestBody(init).collection === "agents" ? [agent, devops] : [],
        })
      : json({ conversation_id: "con_shared" }),
  );
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(<Compose onClose={vi.fn()} />, { wrapper: Wrapper });
  fireEvent.click(
    await screen.findByRole("checkbox", { name: /Research assistant/ }),
  );
  fireEvent.click(screen.getByRole("checkbox", { name: /DevOps 2/ }));
  fireEvent.click(screen.getByRole("button", { name: "Create group chat" }));
  const input = screen.getByRole("textbox", { name: "Message" });
  fireEvent.change(input, { target: { value: "@Dev", selectionStart: 4 } });
  expect(screen.getByRole("option", { name: /DevOps 2/ })).toBeInTheDocument();
  fireEvent.keyDown(input, { key: "Enter" });
  expect(input).toHaveValue("@DevOps 2 ");
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() =>
    expect(
      fn.mock.calls.some(([path]) => path.endsWith("/conversations")),
    ).toBe(true),
  );
  const call = fn.mock.calls.find(([path]) => path.endsWith("/conversations"))!;
  expect(requestBody(call[1]).mentions).toEqual([
    { principal_id: devops.id, start: 0, end: 9 },
  ]);
  expect(requestBody(call[1]).targets).toEqual([
    { kind: "agent", id: agent.id },
    { kind: "agent", id: devops.id },
  ]);
});
it("keeps follow-ups in the same conversation", async () => {
  const fn = transport(),
    { Wrapper } = harness();
  render(
    <Compose
      conversation="con_existing"
      initial={[{ kind: "agent", id: agent.id }]}
      onClose={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.change(screen.getByRole("textbox", { name: "Message" }), {
    target: { value: "How long will staging be busy?" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() =>
    expect(fn.mock.calls.some(([p]) => p.endsWith("/messages"))).toBe(true),
  );
  expect(
    requestBody(fn.mock.calls.find(([p]) => p.endsWith("/messages"))![1])
      .conversation_id,
  ).toBe("con_existing");
});

it("attaches a removable approval preview without claiming it was sent", () => {
  transport();
  const { Wrapper } = harness();
  render(
    <Compose initial={[{ kind: "agent", id: agent.id }]} onClose={vi.fn()} />,
    { wrapper: Wrapper },
  );
  fireEvent.click(screen.getByRole("button", { name: "Request approval" }));
  expect(screen.getByLabelText("Approval flow preview")).toBeInTheDocument();
  for (const label of ["Approve", "Deny", "Ask for context"])
    expect(screen.getByText(label, { exact: true })).toBeInTheDocument();
  expect(screen.queryByText("Approval requested")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Remove approval flow" }));
  expect(
    screen.getByRole("button", { name: "Request approval" }),
  ).toBeInTheDocument();
});
it("defaults multi-recipient messages to private conversations", async () => {
  const fn = transport(),
    { Wrapper } = harness();
  render(
    <Compose
      initial={[
        { kind: "agent", id: agent.id },
        { kind: "agent", id: "agt_two" },
      ]}
      onClose={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  expect(
    screen.getByRole("button", { name: "Send individually" }),
  ).toHaveAttribute("aria-pressed", "true");
  fireEvent.change(screen.getByRole("textbox", { name: "Message" }), {
    target: { value: "Check the release blockers" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() =>
    expect(fn.mock.calls.some(([p]) => p.endsWith("/message-batches"))).toBe(
      true,
    ),
  );
  expect(fn.mock.calls.some(([p]) => p.endsWith("/conversations"))).toBe(false);
});
