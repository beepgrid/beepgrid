import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { PreferenceEditor } from "../src/pages/DeliveryPreferences";
import { harness, json, requestBody, me, agent } from "./fixtures";
import type {
  DeliveryPreferences,
  Destination,
  Connection,
  Group,
} from "../src/contracts";
const destinations: Destination[] = [0, 1, 2].map((i) => ({
  id: `dest_${i}`,
  connection_id: `cn_${i}`,
  principal_id: me.person.id,
  address: `address_${i}`,
  mode: i ? "fallback" : "primary",
  priority: i,
  version: 1,
}));
const connections: Connection[] = [0, 1, 2].map((i) => ({
  id: `cn_${i}`,
  name: ["Slack", "Telegram", "Email"][i],
  kind: ["slack", "telegram", "email"][i],
  enabled: true,
  config: {},
  secret_configured: true,
  version: 1,
}));
const value: DeliveryPreferences = {
  person_id: me.person.id,
  version: 2,
  default: {
    mode: "preferred",
    destinations: ["dest_0", "dest_1"],
    followup_seconds: 0,
  },
  overrides: [],
};
const roles: Group[] = [
  {
    id: "grp_builders",
    name: "Builders",
    purpose: "agent_role",
    members: [],
    members_complete: true,
    member: false,
    version: 1,
    created_by: me.person.id,
  },
];
afterEach(() => vi.unstubAllGlobals());
function mount(v = value) {
  const { Wrapper } = harness();
  render(
    <PreferenceEditor
      value={v}
      destinations={destinations}
      connections={connections}
      agents={[agent as never]}
      roles={roles}
    />,
    { wrapper: Wrapper },
  );
}
it("reorders the stack by drag and keyboard, and saves the selected order", async () => {
  const fn = vi.fn(async (_path: string, _init?: RequestInit) =>
    json({ ...value, version: 3 }),
  );
  vi.stubGlobal("fetch", fn);
  mount();
  const transfer = { effectAllowed: "", setData: vi.fn() };
  fireEvent.dragStart(screen.getByRole("button", { name: "Move channel 1" }), {
    dataTransfer: transfer,
  });
  fireEvent.drop(
    screen.getByRole("button", { name: "Move channel 2" }).closest("li")!,
  );
  expect(screen.getByLabelText("Channel 1")).toHaveValue("dest_1");
  fireEvent.keyDown(screen.getByRole("button", { name: "Move channel 1" }), {
    key: "ArrowDown",
  });
  expect(screen.getByLabelText("Channel 1")).toHaveValue("dest_0");
  fireEvent.keyDown(screen.getByRole("button", { name: "Move channel 2" }), {
    key: "ArrowUp",
  });
  fireEvent.click(screen.getByRole("button", { name: "Save preferences" }));
  await waitFor(() => expect(fn).toHaveBeenCalled());
  expect(requestBody(fn.mock.calls[0][1]).default.destinations).toEqual([
    "dest_1",
    "dest_0",
  ]);
});
it("creates an optional Agent override and can restore inheritance", async () => {
  const fn = vi.fn(async (_path: string, _init?: RequestInit) =>
    json({ ...value, version: 3 }),
  );
  vi.stubGlobal("fetch", fn);
  mount();
  fireEvent.change(screen.getByLabelText("Preferences for"), {
    target: { value: "agent:" + agent.id },
  });
  expect(screen.getByRole("checkbox", { name: "Use default" })).toBeChecked();
  expect(screen.getByLabelText("Channel 1")).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox", { name: "Use default" }));
  fireEvent.change(screen.getByLabelText("Channel 1"), {
    target: { value: "dest_2" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save preferences" }));
  await waitFor(() => expect(fn).toHaveBeenCalled());
  const body = requestBody(fn.mock.calls[0][1]);
  expect(body.default).toEqual(value.default);
  expect(body.overrides[0]).toEqual({
    source: { kind: "agent", id: agent.id },
    policy: { ...value.default, destinations: ["dest_2", "dest_1"] },
  });
});
it("offers timers only for preferred routing and clears them for all-channel delivery", () => {
  mount();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /If I haven’t responded/ }),
  );
  expect(screen.getByRole("combobox", { name: "Follow-up delay" })).toHaveValue(
    "900",
  );
  fireEvent.change(screen.getByLabelText("Delivery"), {
    target: { value: "all" },
  });
  expect(
    screen.queryByRole("checkbox", { name: /If I haven’t responded/ }),
  ).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Delivery"), {
    target: { value: "preferred" },
  });
  expect(
    screen.getByRole("checkbox", { name: /If I haven’t responded/ }),
  ).not.toBeChecked();
});
it("saves a seconds-based preset rather than a minute conversion", async () => {
  const fn = vi.fn(async (_path: string, _init?: RequestInit) =>
    json({ ...value, version: 3 }),
  );
  vi.stubGlobal("fetch", fn);
  mount();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /If I haven’t responded/ }),
  );
  const delay = screen.getByRole("combobox", { name: "Follow-up delay" });
  expect(Array.from(delay.querySelectorAll("option"), (o) => o.value)).toEqual([
    "5",
    "15",
    "30",
    "60",
    "180",
    "300",
    "600",
    "900",
  ]);
  fireEvent.change(delay, { target: { value: "5" } });
  fireEvent.click(screen.getByRole("button", { name: "Save preferences" }));
  await waitFor(() => expect(fn).toHaveBeenCalled());
  expect(requestBody(fn.mock.calls[0][1]).default.followup_seconds).toBe(5);
});
it("preserves a previously saved longer delay until the user chooses a preset", async () => {
  const fn = vi.fn(async (_path: string, _init?: RequestInit) =>
    json({ ...value, version: 3 }),
  );
  vi.stubGlobal("fetch", fn);
  mount({ ...value, default: { ...value.default!, followup_seconds: 3600 } });
  const delay = screen.getByRole("combobox", { name: "Follow-up delay" });
  expect(delay).toHaveValue("3600");
  expect(
    screen.getByRole("option", { name: "60 minutes (current)" }),
  ).toBeDisabled();
  fireEvent.keyDown(screen.getByRole("button", { name: "Move channel 1" }), {
    key: "ArrowDown",
  });
  fireEvent.click(screen.getByRole("button", { name: "Save preferences" }));
  await waitFor(() => expect(fn).toHaveBeenCalled());
  expect(requestBody(fn.mock.calls[0][1]).default.followup_seconds).toBe(3600);
});
it("keeps all-channel opt-outs explicit and can use workspace history only", async () => {
  const fn = vi.fn(async (_path: string, _init?: RequestInit) =>
    json({ ...value, version: 3 }),
  );
  vi.stubGlobal("fetch", fn);
  mount();
  fireEvent.click(screen.getByRole("button", { name: "Remove channel 2" }));
  fireEvent.click(screen.getByRole("button", { name: "Remove channel 1" }));
  expect(screen.getByText(/Only in Beepgrid/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Save preferences" }));
  await waitFor(() => expect(fn).toHaveBeenCalled());
  expect(requestBody(fn.mock.calls[0][1]).default.destinations).toEqual([]);
});
it("can remove an override after its Agent becomes unavailable", async () => {
  const fn = vi.fn(async (_path: string, _init?: RequestInit) =>
    json({ ...value, version: 3 }),
  );
  vi.stubGlobal("fetch", fn);
  mount({
    ...value,
    overrides: [
      { source: { kind: "agent", id: "removed" }, policy: value.default! },
    ],
  });
  fireEvent.change(screen.getByLabelText("Preferences for"), {
    target: { value: "agent:removed" },
  });
  expect(screen.getByText(/This contact is unavailable/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("checkbox", { name: "Use default" }));
  fireEvent.click(screen.getByRole("button", { name: "Save preferences" }));
  await waitFor(() => expect(fn).toHaveBeenCalled());
  expect(requestBody(fn.mock.calls[0][1]).overrides).toEqual([]);
});
