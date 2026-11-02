import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ChatMembers } from "../src/ChatMembers";
import type { Conversation, Identity } from "../src/contracts";
import { agent, harness, json, me, requestBody } from "./fixtures";
const extra = { ...agent, id: "agt_extra", name: "DevOps 2" } as Identity;
const chat: Conversation = {
  id: "con_shared",
  title: "Checkout rollout",
  created_by: me.person.id,
  manager_id: me.person.id,
  shared: true,
  version: 3,
  created_at: "2026-10-30T00:00:00Z",
  participants: [me.person, agent as Identity],
};
afterEach(() => vi.unstubAllGlobals());
function transport() {
  const fn = vi.fn(async (path: string, init?: RequestInit) =>
    path.endsWith("/query")
      ? json({
          items:
            requestBody(init).collection === "agents" ? [agent, extra] : [],
        })
      : json({
          conversation_id: chat.id,
          version: 4,
          left: requestBody(init).operation === "leave",
        }),
  );
  vi.stubGlobal("fetch", fn);
  return fn;
}
it("adds individual members with a deliberate earlier-history choice, off by default", async () => {
  const fn = transport(),
    { Wrapper } = harness(),
    done = vi.fn();
  render(
    <ChatMembers
      chat={chat}
      onClose={vi.fn()}
      onChanged={done}
      onLeft={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.click(screen.getByRole("button", { name: "Add members" }));
  const history = screen.getByRole("checkbox", {
    name: /Share earlier history/,
  });
  expect(history).not.toBeChecked();
  expect(
    screen.getByText("New members will see messages from the time they join."),
  ).toBeInTheDocument();
  const candidate = await screen.findByRole("checkbox", { name: /DevOps 2/ });
  expect(
    screen.queryByRole("checkbox", { name: /Research assistant/ }),
  ).not.toBeInTheDocument();
  fireEvent.click(candidate);
  fireEvent.click(screen.getByRole("button", { name: "Add members" }));
  await waitFor(() => expect(done).toHaveBeenCalled());
  const call = fn.mock.calls.find(([p]) => p.endsWith("/membership"));
  expect(requestBody(call![1])).toEqual({
    operation: "add",
    version: 3,
    members: [extra.id],
    share_history: false,
  });
});
it("explicitly shares earlier files and decisions only when requested", async () => {
  const fn = transport(),
    { Wrapper } = harness();
  render(
    <ChatMembers
      chat={chat}
      onClose={vi.fn()}
      onChanged={vi.fn()}
      onLeft={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.click(screen.getByRole("button", { name: "Add members" }));
  fireEvent.click(await screen.findByRole("checkbox", { name: /DevOps 2/ }));
  fireEvent.click(
    screen.getByRole("checkbox", { name: /Share earlier history/ }),
  );
  expect(
    screen.getByText(
      "New members will see earlier messages, decisions and files.",
    ),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Add members" }));
  await waitFor(() =>
    expect(fn.mock.calls.some(([p]) => p.endsWith("/membership"))).toBe(true),
  );
  expect(
    requestBody(fn.mock.calls.find(([p]) => p.endsWith("/membership"))![1])
      .share_history,
  ).toBe(true);
});
it("requires a current successor before the chat manager leaves", async () => {
  const fn = transport(),
    { Wrapper } = harness(),
    left = vi.fn();
  render(
    <ChatMembers
      chat={chat}
      onClose={vi.fn()}
      onChanged={vi.fn()}
      onLeft={left}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.click(screen.getByRole("button", { name: "Leave chat" }));
  expect(screen.getByRole("button", { name: "Leave chat" })).toBeDisabled();
  fireEvent.change(screen.getByRole("combobox", { name: "New chat manager" }), {
    target: { value: agent.id },
  });
  fireEvent.click(screen.getByRole("button", { name: "Leave chat" }));
  await waitFor(() => expect(left).toHaveBeenCalled());
  expect(
    requestBody(fn.mock.calls.find(([p]) => p.endsWith("/membership"))![1]),
  ).toMatchObject({ operation: "leave", version: 3, next_manager: agent.id });
});
it("keeps management controls out of another member's panel", () => {
  transport();
  const { Wrapper } = harness();
  render(
    <ChatMembers
      chat={{ ...chat, manager_id: agent.id }}
      onClose={vi.fn()}
      onChanged={vi.fn()}
      onLeft={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  expect(
    screen.queryByRole("button", { name: "Rename" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Remove" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Add members" }),
  ).not.toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Leave chat" }),
  ).toBeInTheDocument();
});
