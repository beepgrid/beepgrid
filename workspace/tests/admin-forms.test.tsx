import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ConnectionForm } from "../src/pages/Integrations";
import { RoleForm } from "../src/pages/Permissions";
import { harness, json, requestBody } from "./fixtures";
afterEach(() => vi.unstubAllGlobals());
it("preserves stored installation credentials on an ordinary versioned edit", async () => {
  const fetcher = vi.fn(async (path: string, init?: RequestInit) =>
    json({ id: "cn_1" }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(
    <ConnectionForm
      provider={{
        kind: "webhook",
        fields: [
          { name: "url", required: true, secret: false },
          { name: "secret", required: true, secret: true },
        ],
        inbound_messages: true,
        native_approvals: false,
        web_continuation: true,
        attachments: false,
        builtin: false,
      }}
      connection={{
        id: "cn_1",
        name: "Hook",
        kind: "webhook",
        config: { url: "https://example.com/hook" },
        enabled: true,
        secret_configured: true,
        version: 9,
      }}
      onClose={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  expect(screen.queryByDisplayValue("stored-secret")).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole("textbox", { name: "Connection name" }), {
    target: { value: "Renamed hook" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save connection" }));
  await waitFor(() => expect(fetcher).toHaveBeenCalled());
  expect(requestBody(fetcher.mock.calls[0][1])).toMatchObject({
    id: "cn_1",
    version: 9,
    name: "Renamed hook",
  });
  expect(requestBody(fetcher.mock.calls[0][1])).not.toHaveProperty("secret");
});
it("previews role edits with their observed version and refuses a prohibited save", async () => {
  const fetcher = vi.fn(async (path: string, init?: RequestInit) =>
    path.endsWith("/workspace")
      ? json({
          permission_model: { management_targets: { group: "Exact group ID" } },
        })
      : json({ can_save: false, affected_assignments: [], checks: [] }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(
    <RoleForm
      role={{
        id: "rol_1",
        name: "Bounded role",
        version: 7,
        rules: [{ action: "view", target_kind: "agent", target_id: "agt_one" }],
      }}
      onClose={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.change(screen.getByRole("textbox", { name: "Role name" }), {
    target: { value: "Broader proposal" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Preview changes" }));
  await screen.findByRole("heading", { name: "Policy preview" });
  const call = fetcher.mock.calls.find(([p]) => p.endsWith("/roles/preview"))!;
  expect(requestBody(call[1]).role).toMatchObject({
    id: "rol_1",
    version: 7,
    name: "Broader proposal",
  });
  expect(
    screen.getByRole("button", { name: "Save reviewed role" }),
  ).toBeDisabled();
  expect(fetcher.mock.calls.some(([p]) => p.endsWith("/roles"))).toBe(false);
});
