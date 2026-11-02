import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import Directory from "../src/pages/Directory";
import Groups from "../src/pages/Groups";
import Inbox from "../src/pages/Inbox";
import { agent, harness, json, requestBody } from "./fixtures";

afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  history.replaceState(null, "", "/agents");
});
function collection(items: unknown[] = []) {
  return { items, order: "id_ascending" };
}
it("view-only discovery does not offer messaging, requests or management", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const b = requestBody(init);
      if (path.endsWith("/query"))
        return json(collection(b.collection === "agents" ? [agent] : []));
      if (path.endsWith("/capabilities"))
        return json({
          operations: b.target?.id
            ? {
                view: true,
                inspect: true,
                message: false,
                ask: false,
                manage: false,
              }
            : {},
          outcomes: [],
        });
      return json([]);
    }),
  );
  const { Wrapper } = harness();
  render(<Directory section="agents" />, { wrapper: Wrapper });
  fireEvent.click(
    await screen.findByRole("button", { name: /Research assistant/ }),
  );
  await screen.findByRole("tab", { name: "Access" });
  expect(
    screen.queryByRole("button", { name: "Message" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Ask or request" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Disable access" }),
  ).not.toBeInTheDocument();
});
it("a delegated nonadmin can edit a scoped person without editing external identity", async () => {
  const person = {
    ...agent,
    id: "per_managed",
    kind: "person",
    name: "Managed teammate",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const b = requestBody(init);
      if (path.endsWith("/query"))
        return json(collection(b.collection === "people" ? [person] : []));
      if (path.endsWith("/capabilities"))
        return json({
          operations: b.target?.id
            ? { view: true, manage: true }
            : { create_person: false },
          outcomes: [],
        });
      return json([]);
    }),
  );
  const { Wrapper } = harness();
  render(<Directory section="people" />, { wrapper: Wrapper });
  fireEvent.click(
    await screen.findByRole("button", { name: /Managed teammate/ }),
  );
  fireEvent.click(await screen.findByRole("button", { name: "Edit profile" }));
  await screen.findByRole("heading", { name: "Edit person" });
  expect(screen.getByRole("textbox", { name: "Name" })).toHaveValue(
    "Managed teammate",
  );
  expect(
    screen.queryByRole("textbox", { name: "External ID" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Add person" }),
  ).not.toBeInTheDocument();
});
it("a partial group roster offers only a safe name edit", async () => {
  const group = {
    id: "grp_one",
    name: "Product",
    purpose: "team",
    version: 8,
    member: true,
    members: ["per_self"],
    members_complete: false,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) =>
      path.endsWith("/role-bindings")
        ? json([])
        : path.endsWith("/query")
          ? json(collection([group]))
          : json({ operations: { view: true, manage: true }, outcomes: [] }),
    ),
  );
  const { Wrapper } = harness();
  render(<Groups />, { wrapper: Wrapper });
  fireEvent.click(await screen.findByRole("button", { name: /Product/ }));
  fireEvent.click(await screen.findByRole("button", { name: "Edit team" }));
  expect(
    await screen.findByRole("button", { name: "Save team" }),
  ).toBeEnabled();
  expect(
    screen.queryByRole("combobox", { name: "Recipient type" }),
  ).not.toBeInTheDocument();
  expect(screen.getByText(/full roster is not visible/)).toBeInTheDocument();
});
it("tasks expose all eligible outcomes and preserve the observed request version", async () => {
  history.replaceState(null, "", "/requests/req_task");
  let resolved = false;
  const calls: { path: string; body: unknown }[] = [];
  const r = {
    id: "req_task",
    requester_id: "agt_scout",
    conversation_id: "con_one",
    kind: "task",
    title: "Check the report",
    body: "Report the actual result.",
    state: "open",
    version: 7,
    choices: [],
    created_at: "2026-10-25T00:00:00Z",
    expires_at: "2027-01-01T00:00:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const b = requestBody(init);
      calls.push({ path, body: b });
      if (path.endsWith("/responses")) {
        resolved = true;
        return json({
          ...r,
          state: "resolved",
          response: { outcome: b.outcome, answer: b.answer },
        });
      }
      if (path.endsWith("/capabilities"))
        return json({
          operations: b.request_id ? { view: true, respond: !resolved } : {},
          request_version: 7,
          outcomes: resolved ? [] : ["completed", "failed", "rejected"],
        });
      if (path.endsWith("/query")) return json(collection());
      if (path.endsWith("/trace"))
        return json({ evidence: "Transport is separate from execution." });
      if (path.includes("/conversations/"))
        return json({ id: "con_one", participants: [agent] });
      return json({ ...r, state: resolved ? "resolved" : "open" });
    }),
  );
  const { Wrapper } = harness();
  render(<Inbox />, { wrapper: Wrapper });
  await screen.findByRole("button", { name: "Mark completed" });
  expect(screen.getByRole("button", { name: "Report failed" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Reject task" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Report failed" }));
  await waitFor(() =>
    expect(
      calls.find((c) => c.path.endsWith("/responses"))?.body,
    ).toMatchObject({ version: 7, outcome: "failed" }),
  );
});
it("approval content is escaped and no decision appears without eligibility", async () => {
  history.replaceState(null, "", "/requests/req_approval");
  const r = {
    id: "req_approval",
    requester_id: "agt_scout",
    conversation_id: "con_one",
    kind: "approval",
    title: "Approve release",
    body: "<script>unsafe()</script>",
    state: "open",
    version: 3,
    choices: [],
    action: {
      name: "deploy",
      executor_id: "svc_deploy",
      resource: "release/42",
      resource_version: "sha-123",
      arguments: { amount_minor: 2500, currency: "USD" },
      operation_key: "release-once",
    },
    created_at: "2026-10-25T00:00:00Z",
    expires_at: "2027-01-01T00:00:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (path.endsWith("/capabilities"))
        return json({
          operations: { view: true, respond: false },
          request_version: 3,
          outcomes: [],
        });
      if (path.endsWith("/query")) return json(collection());
      if (path.endsWith("/trace"))
        return json({ evidence: "Transport is separate." });
      if (path.includes("/conversations/"))
        return json({ id: "con_one", participants: [agent] });
      return json(r);
    }),
  );
  const { Wrapper } = harness();
  render(<Inbox />, { wrapper: Wrapper });
  await screen.findByText("<script>unsafe()</script>");
  expect(screen.getByText("<script>unsafe()</script>")).toBeVisible();
  expect(document.querySelector("script")).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Approve" }),
  ).not.toBeInTheDocument();
});

it("does not present a restricted policy grant as unrestricted contact access", async () => {
  history.replaceState(null, "", "/settings/contacts");
  const policy = {
    id: "rol_limited",
    name: "Staging only",
    version: 1,
    rules: [
      {
        action: "ask",
        target_kind: "agent",
        target_id: agent.id,
        action_name: "deploy",
        conditions: { resource: "staging" },
      },
    ],
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const body = requestBody(init);
      if (path.includes("/access?"))
        return json({
          subject: { id: "per_self", name: "Taylor", kind: "person" },
          basis: "role",
          roles: [
            { role: policy, subject_kind: "principal", subject_id: "per_self" },
          ],
        });
      if (path.endsWith("/role-bindings")) return json([]);
      if (path.endsWith("/query"))
        return json(
          collection(
            body.collection === "roles"
              ? [policy]
              : body.collection === "agents"
                ? [agent]
                : [],
          ),
        );
      return json({ operations: {}, outcomes: [] });
    }),
  );
  const { default: Permissions } = await import("../src/pages/Permissions");
  const { Wrapper } = harness();
  render(<Permissions />, { wrapper: Wrapper });
  expect(await screen.findByText("Limited")).toBeInTheDocument();
  expect(screen.getByText("Research assistant")).toBeInTheDocument();
  expect(screen.queryByText("agt_scout")).not.toBeInTheDocument();
});
