import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Compose } from "../src/Compose";
import { harness, json, requestBody } from "./fixtures";
afterEach(() => vi.unstubAllGlobals());
it("addresses one role without expanding its members and retains a queued receipt", async () => {
  const transport = vi.fn(async (path: string, init?: RequestInit) => {
    if (path.endsWith("/query"))
      return json({
        items:
          requestBody(init).collection === "groups"
            ? [
                {
                  id: "grp_builder",
                  name: "Builder",
                  purpose: "agent_role",
                  members: ["a", "b"],
                },
              ]
            : [],
      });
    return json({
      id: "acast_one",
      role_id: "grp_builder",
      role_name: "Builder",
      state: "queued",
      input: { body: "Inspect the API" },
    });
  });
  vi.stubGlobal("fetch", transport);
  const { Wrapper } = harness();
  const close = vi.fn();
  render(<Compose onClose={close} />, { wrapper: Wrapper });
  fireEvent.click(screen.getByRole("button", { name: "Anyone available" }));
  await screen.findByRole("option", { name: "Builder" });
  fireEvent.change(screen.getByLabelText("Agent role"), {
    target: { value: "grp_builder" },
  });
  const editor = screen.getByRole("textbox", { name: "Message" });
  fireEvent.change(editor, { target: { value: "Inspect the API" } });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await screen.findByText("Waiting for an agent");
  const calls = transport.mock.calls.filter(([path]) =>
    path.endsWith("/anycast"),
  );
  expect(calls).toHaveLength(1);
  expect(requestBody(calls[0][1])).toEqual({
    role_id: "grp_builder",
    body: "Inspect the API",
    attachment_ids: [],
    kind: "message",
    choices: [],
  });
  expect(
    transport.mock.calls.some(
      ([path]) =>
        path.endsWith("/messages") || path.endsWith("/message-batches"),
    ),
  ).toBe(false);
  expect(close).not.toHaveBeenCalled();
});
