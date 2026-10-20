// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { afterEach, it, expect, vi } from "vitest";
import { Client } from "@notif/client";
import { NotifRequest } from "./index";
afterEach(cleanup);
it("clarification preserves a pending request and an authorized answer records its version", async () => {
  const respond = vi.fn(async () => ({
    ...q,
    state: "resolved",
    response: { outcome: "answered", answer: "Brief", version: 3 },
  }));
  const q = {
    id: "req_1",
    conversation_id: "con_1",
    kind: "question",
    title: "Which format?",
    body: "Choose one",
    choices: ["Brief", "Detailed"],
    state: "open",
    version: 3,
  };
  const client = {
    getRequest: vi.fn(async () => q),
    respond,
  } as unknown as Client;
  const open = vi.fn();
  render(<NotifRequest client={client} id="req_1" onConversation={open} />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Ask for clarification" }),
  );
  expect(open).toHaveBeenCalledWith("con_1");
  expect(respond).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("radio", { name: "Brief" }));
  fireEvent.click(screen.getByRole("button", { name: "Send answer" }));
  await waitFor(() =>
    expect(respond).toHaveBeenCalledWith("req_1", {
      outcome: "answered",
      answer: "Brief",
      version: 3,
    }),
  );
  expect(await screen.findByText("answered: Brief")).toBeVisible();
});
it("a denied response preserves the question and shows the server error", async () => {
  const client = {
    getRequest: async () => ({
      id: "q",
      kind: "approval",
      title: "Deploy?",
      state: "open",
      version: 1,
    }),
    respond: async () => {
      throw new Error("This operation is not permitted.");
    },
  } as unknown as Client;
  render(<NotifRequest client={client} id="q" />);
  fireEvent.click(await screen.findByRole("button", { name: "Approve" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("not permitted");
  expect(screen.getByRole("button", { name: "Approve" })).toBeEnabled();
});
