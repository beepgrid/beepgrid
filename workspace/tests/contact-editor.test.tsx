import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import Permissions from "../src/pages/Permissions";
import type { ContactPreview } from "../src/pages/Permissions";
import type { Rule, Identity } from "../src/contracts";
import { agent, harness, json, requestBody } from "./fixtures";

afterEach(() => {
  vi.unstubAllGlobals();
  history.replaceState(null, "", "/agents");
});
function setup(inherited = true) {
  const peer = { ...agent, id: "agt_builder", name: "Builder" };
  const saved: Record<string, unknown>[] = [];
  let version = 1,
    fail = false;
  const mock = vi.fn(async (path: string, init?: RequestInit) => {
    const body = requestBody(init);
    if (path.endsWith("/contact-permissions/preview")) {
      if (body.rules && body.version !== version)
        return json(
          {
            error: {
              code: "VERSION_CONFLICT",
              message: "Permissions changed. Reload before saving.",
            },
          },
          409,
        );
      const rules: Rule[] = body.rules || [];
      const permissions = Object.fromEntries(
        ["view", "message", "ask", "decide"].map((action) => {
          const r = rules.find((r) => r.action === action);
          const blocked = r?.effect === "deny";
          const allowed =
            !blocked &&
            (!!r || (inherited && ["view", "message"].includes(action)));
          return [
            action,
            {
              allowed,
              blocked,
              limited: false,
              direct: (r ? (blocked ? "deny" : "allow") : "default") as
                "default" | "allow" | "deny",
              sources: blocked
                ? ["Blocked directly"]
                : r
                  ? ["Allowed directly"]
                  : allowed
                    ? ["Via Engineering"]
                    : [],
            },
          ];
        }),
      );
      return json({
        policy: {
          id: "rol_contacts_one",
          name: "Contacts",
          version,
          rules: [],
        },
        contacts: [
          {
            target: { kind: "agent", id: peer.id },
            name: peer.name,
            permissions,
          },
        ],
        warnings: [],
        can_save: true,
      } satisfies ContactPreview);
    }
    if (path.endsWith("/contact-permissions")) {
      if (fail) {
        version = 2;
        return json(
          {
            error: {
              code: "VERSION_CONFLICT",
              message: "Permissions changed. Reload before saving.",
            },
          },
          409,
        );
      }
      saved.push(body);
      return json({ version: version + 1 });
    }
    if (path.includes("/access?"))
      return json({ basis: "effective_role_grants", roles: [] });
    if (path.endsWith("/role-bindings")) return json([]);
    if (path.endsWith("/query"))
      return json({
        items: body.collection === "agents" ? [agent, peer] : [],
        order: "id_ascending",
      });
    return json({
      operations: { manage: true, create_role: true },
      outcomes: [],
    });
  });
  vi.stubGlobal("fetch", mock);
  const { Wrapper } = harness();
  render(
    <Permissions profile={{ ...agent, kind: "agent" } as Identity} embedded />,
    { wrapper: Wrapper },
  );
  return {
    saved,
    conflict: () => {
      fail = true;
    },
    mock,
  };
}
it("turns inherited access into a reviewed explicit block without granting reverse access", async () => {
  const { saved } = setup();
  await openEditor();
  const dialog = await screen.findByRole("dialog");
  const message = within(dialog).getByRole("checkbox", {
    name: "Message Builder",
  });
  expect(message).toBeChecked();
  expect(within(dialog).getAllByText("Via Engineering")).toHaveLength(2);
  fireEvent.click(message);
  expect(message).not.toBeChecked();
  expect(within(dialog).getByText("Blocked")).toBeVisible();
  const save = within(dialog).getByRole("button", { name: "Save permissions" });
  await waitFor(() => expect(save).toBeEnabled());
  fireEvent.click(save);
  expect(saved).toHaveLength(0);
  expect(within(dialog).getByText("Message: blocked")).toBeVisible();
  fireEvent.click(
    within(dialog).getByRole("button", { name: "Confirm changes" }),
  );
  await waitFor(() => expect(saved).toHaveLength(1));
  expect(saved[0]).toMatchObject({
    subject: { kind: "agent", id: agent.id },
    version: 1,
    rules: [
      {
        action: "message",
        target_kind: "agent",
        target_id: "agt_builder",
        effect: "deny",
      },
    ],
  });
});
it("restores inherited defaults and makes visibility dependencies explicit", async () => {
  setup();
  await openEditor();
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(
    within(dialog).getByRole("checkbox", { name: "Message Builder" }),
  );
  await waitFor(() =>
    expect(
      within(dialog).getByRole("button", { name: "Use defaults" }),
    ).toBeVisible(),
  );
  fireEvent.click(within(dialog).getByRole("button", { name: "Use defaults" }));
  await waitFor(() =>
    expect(
      within(dialog).getByRole("checkbox", { name: "Message Builder" }),
    ).toBeChecked(),
  );
  expect(
    within(dialog).getByRole("button", { name: "Save permissions" }),
  ).toBeDisabled();
  fireEvent.click(
    within(dialog).getByRole("checkbox", { name: "See Builder" }),
  );
  for (const label of [
    "See",
    "Message",
    "Request a reply",
    "Approve / decline",
  ])
    expect(
      within(dialog).getByRole("checkbox", { name: `${label} Builder` }),
    ).not.toBeChecked();
});
it("enabling requests includes visibility without enabling approval", async () => {
  setup(false);
  await openEditor();
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(
    within(dialog).getByRole("checkbox", { name: "Request a reply Builder" }),
  );
  expect(
    within(dialog).getByRole("checkbox", { name: "See Builder" }),
  ).toBeChecked();
  expect(
    within(dialog).getByRole("checkbox", { name: "Approve / decline Builder" }),
  ).not.toBeChecked();
  expect(
    within(dialog).getByRole("checkbox", { name: "Message Builder" }),
  ).not.toBeChecked();
});
it("reloads changed permissions after a concurrent save without silently overwriting", async () => {
  const { conflict, saved } = setup(false);
  await openEditor();
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(
    within(dialog).getByRole("checkbox", { name: "Request a reply Builder" }),
  );
  const save = within(dialog).getByRole("button", { name: "Save permissions" });
  await waitFor(() => expect(save).toBeEnabled());
  conflict();
  fireEvent.click(save);
  const reload = await within(dialog).findByRole("button", {
    name: "Reload access",
  });
  expect(saved).toHaveLength(0);
  expect(save).toBeDisabled();
  fireEvent.click(reload);
  await waitFor(() =>
    expect(
      within(dialog).queryByRole("button", { name: "Reload access" }),
    ).not.toBeInTheDocument(),
  );
  expect(
    within(dialog).getByRole("checkbox", { name: "Request a reply Builder" }),
  ).not.toBeChecked();
});

async function openEditor() {
  const button = await screen.findByRole("button", {
    name: "Edit contact access",
  });
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
}
