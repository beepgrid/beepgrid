import { useState } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { TargetPicker } from "../src/TargetPicker";
import type { Target } from "../src/contracts";
import { harness, agent, json, requestBody } from "./fixtures";
afterEach(() => vi.unstubAllGlobals());
it("expands a visible role to individual recipients and can deselect it", async () => {
  const members = [
    { ...agent, id: "a1", name: "Builder 1" },
    { ...agent, id: "a2", name: "Builder 2" },
  ];
  const fn = vi.fn(async (_p: string, init?: RequestInit) => {
    const b = requestBody(init);
    return json({
      items:
        b.collection === "groups"
          ? [
              {
                id: "role",
                name: "Builders",
                purpose: "agent_role",
                members: ["a1", "a2"],
                members_complete: true,
              },
            ]
          : b.filters?.group_id
            ? members
            : [],
    });
  });
  vi.stubGlobal("fetch", fn);
  function Picker() {
    const [targets, setTargets] = useState<Target[]>([]);
    return (
      <TargetPicker messaging multiple value={targets} onChange={setTargets} />
    );
  }
  const { Wrapper } = harness();
  render(<Picker />, { wrapper: Wrapper });
  fireEvent.click(screen.getByRole("button", { name: "Roles & teams" }));
  await screen.findByRole("checkbox", { name: /Builders/ });
  fireEvent.click(screen.getByRole("checkbox", { name: /Builders/ }));
  await screen.findByRole("button", { name: "Remove Builder 1" });
  expect(
    screen.getByRole("button", { name: "Remove Builder 2" }),
  ).toBeInTheDocument();
  await waitFor(() =>
    expect(screen.getByRole("checkbox", { name: /Builders/ })).toHaveAttribute(
      "aria-checked",
      "true",
    ),
  );
  fireEvent.click(screen.getByRole("checkbox", { name: /Builders/ }));
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Remove Builder 1" }),
    ).not.toBeInTheDocument(),
  );
});
it("does not expand a role with undisclosed members", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_p: string, init?: RequestInit) =>
      json({
        items:
          requestBody(init).collection === "groups"
            ? [
                {
                  id: "role",
                  name: "Private builders",
                  purpose: "agent_role",
                  members: [],
                  members_complete: false,
                },
              ]
            : [],
      }),
    ),
  );
  const { Wrapper } = harness();
  render(<TargetPicker messaging multiple value={[]} onChange={vi.fn()} />, {
    wrapper: Wrapper,
  });
  fireEvent.click(screen.getByRole("button", { name: "Roles & teams" }));
  fireEvent.click(
    await screen.findByRole("checkbox", { name: /Private builders/ }),
  );
  expect(screen.getByRole("alert")).toHaveTextContent(
    "cannot view every member",
  );
});
