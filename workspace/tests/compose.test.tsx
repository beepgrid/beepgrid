import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Compose, MessageComposer } from "../src/Compose";
import { clearConversationDrafts } from "../src/data";
import type { Request } from "../src/contracts";
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
  for (const label of ["Approve", "Decline", "Ask for context"])
    expect(screen.getByText(label, { exact: true })).toBeInTheDocument();
  const options = screen
    .getByLabelText("Approval flow preview")
    .querySelectorAll(".preview-action");
  expect(options).toHaveLength(3);
  for (const option of options) {
    expect(option.querySelector("svg")).toBeNull();
    expect(option).not.toHaveAttribute("aria-pressed");
  }
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
it("stages a private file and sends its reference with the first message", async () => {
  let acceptUpload: (v: Response) => void = () => {};
  const fn = vi.fn(async (path: string, init?: RequestInit) => {
    if (path.endsWith("/attachments"))
      return new Promise<Response>((resolve) => {
        acceptUpload = resolve;
      });
    if (path.endsWith("/query"))
      return json({
        items: requestBody(init).collection === "agents" ? [agent] : [],
      });
    return json({ conversation_id: "con_file" });
  });
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(
    <Compose initial={[{ kind: "agent", id: agent.id }]} onClose={vi.fn()} />,
    { wrapper: Wrapper },
  );
  fireEvent.change(screen.getByLabelText("Choose files"), {
    target: {
      files: [
        new File(["%PDF-1.4\nfixture"], "orders.pdf", {
          type: "application/pdf",
        }),
      ],
    },
  });
  await screen.findByText("Uploading...");
  expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
  expect(fn.mock.calls.some(([p]) => p.endsWith("/messages"))).toBe(false);
  acceptUpload(json({ id: "file_draft", file_name: "orders.pdf", size: 17 }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() =>
    expect(fn.mock.calls.some(([p]) => p.endsWith("/messages"))).toBe(true),
  );
  expect(
    requestBody(fn.mock.calls.find(([p]) => p.endsWith("/messages"))![1]),
  ).toMatchObject({ attachment_ids: ["file_draft"], body: "" });
  expect(
    fn.mock.calls.find(([p]) => p.endsWith("/attachments"))![1]?.body,
  ).toBeInstanceOf(FormData);
});
it("keeps a failed upload removable without sending or persisting a file reference", async () => {
  const fn = vi.fn(async (path: string, init?: RequestInit) =>
    path.endsWith("/attachments")
      ? json({ error: { message: "Upload unavailable" } }, 503)
      : json({
          items: requestBody(init).collection === "agents" ? [agent] : [],
        }),
  );
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(
    <Compose initial={[{ kind: "agent", id: agent.id }]} onClose={vi.fn()} />,
    { wrapper: Wrapper },
  );
  fireEvent.change(screen.getByLabelText("Choose files"), {
    target: { files: [new File(["x"], "brief.txt", { type: "text/plain" })] },
  });
  await screen.findByRole("alert");
  expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Remove brief.txt" }));
  expect(screen.queryByText("brief.txt")).not.toBeInTheDocument();
  expect(fn.mock.calls.some(([p]) => p.endsWith("/messages"))).toBe(false);
});

it("clears a reply draft and uncertain response on contact loss while preserving another conversation", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      json(
        { error: { code: "UNAVAILABLE", message: "Response uncertain" } },
        503,
      ),
    ),
  );
  const { Wrapper } = harness();
  const view = render(
    <MessageComposer
      conversation="con_private"
      replyTo={{ id: "req_private", version: 3, body: "When?" } as Request}
      replyAuthor="Builder"
    />,
    { wrapper: Wrapper },
  );
  fireEvent.change(screen.getByRole("textbox", { name: "Reply" }), {
    target: { value: "Private response draft" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send reply" }));
  await screen.findByRole("alert");
  expect(
    Object.keys(sessionStorage).some(
      (k) =>
        k.includes(":operation:") &&
        sessionStorage.getItem(k)?.includes("Private response draft"),
    ),
  ).toBe(true);
  view.unmount();
  sessionStorage.setItem(
    "beepgrid:workspace:draft:scope:message:con_other",
    "Keep this other draft",
  );
  clearConversationDrafts("con_private");
  expect(
    Object.keys(sessionStorage).some((k) =>
      sessionStorage.getItem(k)?.includes("Private response draft"),
    ),
  ).toBe(false);
  expect(
    sessionStorage.getItem("beepgrid:workspace:draft:scope:message:con_other"),
  ).toBe("Keep this other draft");
});
