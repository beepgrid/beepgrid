import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import AppearancePicker from "../src/AppearancePicker";
import { harness, json, requestBody } from "./fixtures";

afterEach(() => vi.unstubAllGlobals());

it("saves a presentation choice with the observed role revision, without writing its membership", async () => {
  const closed = vi.fn(),
    writes: any[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      writes.push({ path, body: requestBody(init) });
      return json({ version: 5 });
    }),
  );
  const { Wrapper } = harness();
  render(
    <AppearancePicker
      kind="group"
      id="grp_ops"
      name="DevOps"
      version={4}
      onClose={closed}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.click(screen.getByRole("button", { name: "Icon 8" }));
  fireEvent.click(screen.getByRole("button", { name: "Save icon" }));
  await waitFor(() => expect(closed).toHaveBeenCalledOnce());
  expect(writes).toEqual([
    {
      path: "/api/v1/appearance",
      body: {
        target: { kind: "group", id: "grp_ops" },
        icon: "role-7",
        version: 4,
      },
    },
  ]);
});

it("keeps a stale role choice reviewable and lets an Agent return to its default", async () => {
  const closed = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      json(
        {
          error: {
            code: "CONFLICT",
            message: "This role changed. Reload before saving.",
          },
        },
        409,
      ),
    ),
  );
  const { Wrapper } = harness();
  render(
    <AppearancePicker
      kind="agent"
      id="agt_ops"
      name="DevOps 1"
      icon="mark-3"
      onClose={closed}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.click(screen.getByRole("button", { name: "Use default" }));
  fireEvent.click(screen.getByRole("button", { name: "Save icon" }));
  await screen.findByRole("alert");
  expect(closed).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "Use default" })).toBeDisabled();
});
