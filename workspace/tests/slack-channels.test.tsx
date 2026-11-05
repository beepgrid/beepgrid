import { afterEach, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import {
  SlackChannels,
  SlackChannelPicker,
  SlackDestination,
} from "../src/pages/SlackChannels";
import { harness, json, requestBody } from "./fixtures";

afterEach(() => vi.unstubAllGlobals());
const channel = {
  address: "C1",
  name: "#engineering",
  restricted: true,
  allowed: [{ kind: "group", id: "grp_builders" }],
  version: 4,
};

it("saves channel access with the existing version and Agent role selection", async () => {
  const fn = vi.fn(async (path: string, init?: RequestInit) => {
    if (path.endsWith("/channels"))
      return json(
        init?.method === "POST"
          ? { ...requestBody(init), version: 5 }
          : [channel],
      );
    const input = requestBody(init);
    return json({
      items:
        input.collection === "groups"
          ? [
              {
                id: "grp_builders",
                name: "Engineering",
                purpose: "agent_role",
                members: [],
              },
            ]
          : [
              {
                id: "agt_builder",
                name: "Builder 1",
                kind: "agent",
                disabled: false,
              },
            ],
    });
  });
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(<SlackChannels connection="chn_slack" />, { wrapper: Wrapper });
  fireEvent.click(await screen.findByRole("button", { name: "Manage access" }));
  expect(screen.getByLabelText("Agent access")).toHaveValue("selected");
  expect(
    await screen.findByRole("checkbox", { name: /Engineering/ }),
  ).toBeChecked();
  fireEvent.click(screen.getByRole("checkbox", { name: /Builder 1/ }));
  fireEvent.click(screen.getByRole("button", { name: "Save channel" }));
  await waitFor(() =>
    expect(
      fn.mock.calls.some(
        ([p, init]) => p.endsWith("/channels") && init?.method === "POST",
      ),
    ).toBe(true),
  );
  const call = fn.mock.calls.find(
    ([p, init]) => p.endsWith("/channels") && init?.method === "POST",
  )!;
  expect(requestBody(call[1])).toMatchObject({
    version: 4,
    address: "C1",
    restricted: true,
    allowed: [
      { kind: "group", id: "grp_builders" },
      { kind: "agent", id: "agt_builder" },
    ],
  });
});

it("continues empty Slack pages and selects channel IDs rather than labels", async () => {
  const changed = vi.fn();
  const fn = vi.fn(async (path: string) =>
    json(
      path.endsWith("cursor=")
        ? { channels: [], next_cursor: "next" }
        : { channels: [channel] },
    ),
  );
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(
    <SlackChannelPicker connection="chn_slack" value="" onChange={changed} />,
    { wrapper: Wrapper },
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "Load more channels" }),
  );
  fireEvent.click(await screen.findByRole("radio", { name: "#engineering" }));
  expect(changed).toHaveBeenCalledWith(channel);
  expect(fn.mock.calls.some(([p]) => p.endsWith("cursor=next"))).toBe(true);
});

it("offers configured Slack channels and keeps direct messages available", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => json([channel])),
  );
  const change = vi.fn();
  const { Wrapper } = harness();
  render(
    <SlackDestination connection="chn_slack" address="" onChange={change} />,
    { wrapper: Wrapper },
  );
  expect(screen.getByLabelText("Slack DM channel ID")).toBeVisible();
  fireEvent.change(screen.getByLabelText("Slack destination"), {
    target: { value: "channel" },
  });
  await screen.findByRole("option", { name: "#engineering" });
  fireEvent.change(screen.getByLabelText("Channel"), {
    target: { value: "C1" },
  });
  expect(change).toHaveBeenLastCalledWith("C1");
  expect(screen.getByText(/Everyone in the Slack channel/)).toBeVisible();
});
