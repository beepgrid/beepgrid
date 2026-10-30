import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import AgentContacts from "../src/pages/AgentContacts";
import { agent, harness, json, me, requestBody } from "./fixtures";

afterEach(() => vi.unstubAllGlobals());
it("shows permitted contact/channel context and inspects each directory page", async () => {
  const targets: string[][] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const body = requestBody(init);
      if (path.endsWith("/capabilities"))
        return json({ operations: { view_contact_access: true } });
      if (path.endsWith("/query"))
        return json({
          items: [
            {
              ...me.person,
              id: body.cursor ? "per_second" : "per_first",
              name: body.cursor ? "Sam" : "Maya",
            },
          ],
          ...(body.cursor ? {} : { next_cursor: "page-two" }),
        });
      if (path.endsWith("/contacts")) {
        targets.push(body.targets.map((t: any) => t.id));
        return json({
          contacts: body.targets.map((t: any) => ({
            target: t,
            name: t.id === "per_first" ? "Maya" : "Sam",
            message: true,
            ask: true,
            routes: [
              { kind: "native", enabled: true, mode: "primary" },
              { kind: "slack", enabled: true, mode: "copy" },
            ],
          })),
        });
      }
      return json({});
    }),
  );
  const { Wrapper } = harness();
  render(<AgentContacts agent={{ ...agent, kind: "agent" }} />, {
    wrapper: Wrapper,
  });
  expect(await screen.findByText("Maya")).toBeVisible();
  expect(screen.getByText("Slack · Copy")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Next" }));
  expect(await screen.findByText("Sam")).toBeVisible();
  expect(screen.queryByText("Maya")).not.toBeInTheDocument();
  expect(targets.at(-1)).toEqual(["per_second"]);
  fireEvent.click(screen.getByRole("button", { name: "Previous" }));
  expect(await screen.findByText("Maya")).toBeVisible();
});
it("conceals cached contact context after subject access is revoked", async () => {
  let allowed = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      path.endsWith("/capabilities")
        ? json({ operations: { view_contact_access: allowed } })
        : path.endsWith("/query")
          ? json({ items: [me.person] })
          : json({
              contacts: [
                {
                  target: { kind: "person", id: me.person.id },
                  name: "Private contact",
                  message: true,
                  routes: [],
                },
              ],
            }),
    ),
  );
  const { Wrapper, query } = harness();
  render(<AgentContacts agent={{ ...agent, kind: "agent" }} />, {
    wrapper: Wrapper,
  });
  await screen.findByText("Private contact");
  allowed = false;
  await act(() => query.invalidateQueries());
  await waitFor(() =>
    expect(screen.queryByText("Private contact")).not.toBeInTheDocument(),
  );
  expect(
    screen.getByText("Contact access is restricted to authorized viewers."),
  ).toBeVisible();
});
