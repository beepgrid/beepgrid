import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { AccountView, type Account } from "../src/AccountView";

const account: Account = {
  deployment: "managed",
  applied: {
    company: "Acme",
    environment: "production",
    suspended: true,
    recovery_hold: false,
    control_version: 7,
    metered: true,
    budget: { version: 3, messages: 0, requests: -1, upload_bytes: 2000 },
    usage: [
      { period: "2026-10-01T00:00:00Z", metric: "messages", units: 12 },
      { period: "2026-10-01T00:00:00Z", metric: "requests", units: 3 },
    ],
    source: "runtime",
  },
  managed: {
    id: "tenant_one",
    company: "Acme",
    region: "iad",
    state: "active",
    desired: "active",
    version: 9,
    quota_revision: 5,
    access: {
      enabled: true,
      reason: "Current entitlement",
      status: "active",
      valid_until: 0,
      revision: 8,
    },
  },
};
it("does not confuse an active subscription or requested policy with runtime access and applied limits", () => {
  render(<AccountView account={account} />);
  expect(screen.getByText("Suspended")).toBeInTheDocument();
  expect(screen.getByText("8 requested · 7 applied")).toBeInTheDocument();
  expect(screen.getByText("5 requested · 3 applied")).toBeInTheDocument();
  expect(screen.getByText("No new work")).toBeInTheDocument();
  expect(screen.getByText("Unlimited")).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: /upgrade|pay|activate|suspension/i }),
  ).not.toBeInTheDocument();
});
it("preserves unknown limits and recovery holds instead of presenting zero usage or available access", () => {
  render(
    <AccountView
      account={{
        ...account,
        applied: {
          ...account.applied,
          suspended: false,
          recovery_hold: true,
          budget: undefined,
        },
      }}
    />,
  );
  expect(screen.getByText("Recovery hold")).toBeInTheDocument();
  expect(screen.getAllByText("Unavailable")).toHaveLength(2);
  expect(screen.queryByText("Available")).not.toBeInTheDocument();
});
