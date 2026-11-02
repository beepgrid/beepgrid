import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import AccessTokens from "../src/pages/AccessTokens";
import type { Credential } from "../src/contracts";
import { harness, json, requestBody } from "./fixtures";

afterEach(() => vi.unstubAllGlobals());
const credential: Credential = {
  id: "key_one",
  principal_id: "agt_builder",
  owner_name: "Builder",
  name: "Production worker",
  kind: "worker",
  prefix: "",
  token_hint: "ntf_abcd…1234",
  created_at: "2026-11-01T10:00:00Z",
  expires_at: new Date(Date.now() + 86400000).toISOString(),
  last_used_at: new Date().toISOString(),
  revoked: false,
  version: 4,
  renewable_until: new Date(Date.now() + 90 * 86400000).toISOString(),
};
function setup(
  rows: Credential[] = [credential],
  write: (path: string, body: any) => Response = () => json({ revoked: true }),
) {
  const fetcher = vi.fn(async (path: string, init?: RequestInit) =>
    path.endsWith("/query")
      ? json({ items: rows })
      : path.endsWith("/capabilities")
        ? json({ operations: { issue_credential: true } })
        : write(path, requestBody(init)),
  );
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(<AccessTokens />, { wrapper: Wrapper });
  return fetcher;
}
it("keeps expired tokens out of the active list and exposes meaningful identity and renewal metadata", async () => {
  setup([
    credential,
    {
      ...credential,
      id: "key_expired",
      name: "Old job",
      renewable_until: undefined,
      expires_at: "2020-01-01T00:00:00Z",
      last_used_at: undefined,
    },
  ]);
  expect(await screen.findByText("Production worker")).toBeVisible();
  expect(screen.getByText("Builder")).toBeVisible();
  expect(screen.getByText("Auto-renewing")).toBeVisible();
  expect(screen.queryByText("Old job")).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Token status"), {
    target: { value: "all" },
  });
  expect(screen.getByText("Old job")).toBeVisible();
  expect(screen.getByText("Expired")).toBeVisible();
  fireEvent.change(screen.getByLabelText("Search tokens"), {
    target: { value: "old" },
  });
  expect(screen.queryByText("Production worker")).not.toBeInTheDocument();
});
it("edits the observed version without changing expiry unless requested", async () => {
  const fetcher = setup(
    [{ ...credential, kind: "api", permissions: ["read"] }],
    () => json({ ...credential, version: 5 }),
  );
  await screen.findByText("Production worker");
  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  expect(screen.getByLabelText("Token access")).toHaveValue("read");
  expect(screen.queryByLabelText("Expires")).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Token name"), {
    target: { value: "Reporting job" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([p]) => p.endsWith("/edit"))).toBe(true),
  );
  const call = fetcher.mock.calls.find(([p]) => p.endsWith("/edit"))!;
  expect(requestBody(call[1])).toEqual({
    version: 4,
    name: "Reporting job",
    permissions: ["read"],
  });
});
it("makes never-expiring access a direct expiry choice without sending a fake date", async () => {
  const fetcher = setup([{ ...credential, kind: "api" }], () =>
    json({ ...credential, expires_at: null, version: 5 }),
  );
  await screen.findByText("Production worker");
  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  fireEvent.click(screen.getByLabelText("Change expiry"));
  fireEvent.change(screen.getByLabelText("Expires"), {
    target: { value: "never" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([p]) => p.endsWith("/edit"))).toBe(true),
  );
  const call = fetcher.mock.calls.find(([p]) => p.endsWith("/edit"))!;
  expect(requestBody(call[1])).toEqual({
    version: 4,
    name: "Production worker",
    never_expires: true,
    clear_permissions: true,
  });
});
it("rotates with a bounded overlap and shows the replacement secret only after success", async () => {
  const fetcher = setup([credential], () =>
    json({
      token: "test-replacement-only",
      credential: {
        ...credential,
        version: 5,
        previous_until: new Date(Date.now() + 3600000).toISOString(),
      },
    }),
  );
  await screen.findByText("Production worker");
  fireEvent.click(screen.getByRole("button", { name: "Rotate" }));
  expect(screen.queryByText("test-replacement-only")).not.toBeInTheDocument();
  expect(screen.getByLabelText("Previous token remains valid for")).toHaveValue(
    "3600",
  );
  fireEvent.click(screen.getByRole("button", { name: "Create replacement" }));
  expect(await screen.findByText("test-replacement-only")).toBeVisible();
  const call = fetcher.mock.calls.find(([p]) => p.endsWith("/rotate"))!;
  expect(requestBody(call[1])).toEqual({ version: 4, overlap_seconds: 3600 });
  expect(screen.getByRole("button", { name: "Copy token" })).toBeVisible();
  fireEvent.click(
    screen.getByRole("button", { name: "I have saved it securely" }),
  );
  expect(screen.queryByText("test-replacement-only")).not.toBeInTheDocument();
});
it("keeps the rotation form available after a stale-version error", async () => {
  setup([credential], () =>
    json(
      {
        error: {
          code: "CONFLICT",
          message: "This token changed. Refresh it before rotating.",
        },
      },
      409,
    ),
  );
  await screen.findByText("Production worker");
  fireEvent.click(screen.getByRole("button", { name: "Rotate" }));
  fireEvent.click(screen.getByRole("button", { name: "Create replacement" }));
  expect(
    await screen.findByText("This token changed. Refresh it before rotating."),
  ).toBeVisible();
  expect(screen.getByLabelText("Previous token remains valid for")).toHaveValue(
    "3600",
  );
});
it("requires an explicit choice before stopping connections issued by an enrollment token", async () => {
  const fetcher = setup([{ ...credential, kind: "enrollment" }]);
  await screen.findByText("Production worker");
  fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
  const checkbox = screen.getByLabelText(
    "Also stop connections issued by this token",
  );
  expect(checkbox).not.toBeChecked();
  fireEvent.click(checkbox);
  fireEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", {
      name: "Revoke token",
    }),
  );
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([p]) => p.endsWith("/revoke"))).toBe(true),
  );
  const call = fetcher.mock.calls.find(([p]) => p.endsWith("/revoke"))!;
  expect(requestBody(call[1])).toEqual({ revoke_workers: true });
});
