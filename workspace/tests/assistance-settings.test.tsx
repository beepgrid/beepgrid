import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import Settings from "../src/pages/Settings";
import { harness, json, requestBody } from "./fixtures";

afterEach(() => {
  vi.unstubAllGlobals();
  history.replaceState(null, "", "/agents");
});

it("offers only Coordinator assistance and sends its versioned toggle", async () => {
  history.replaceState(null, "", "/settings/assistance");
  const fetcher = vi.fn(async (path: string, init?: RequestInit) => {
    if (path.endsWith("/assistance/settings")) {
      return json({ overseer: false, overseer_configured: true, version: 4 });
    }
    if (path.endsWith("/capabilities")) {
      return json({ operations: { configure_assistance: true } });
    }
    return json({ workspace: {} });
  });
  vi.stubGlobal("fetch", fetcher);
  const { Wrapper } = harness();
  render(<Settings />, { wrapper: Wrapper });
  await screen.findByRole("heading", { name: "Coordinator" });
  expect(
    screen.queryByText(/Automatic informational answers/i),
  ).not.toBeInTheDocument();
  expect(screen.queryByText(/Matcher configured/i)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Enable" }));
  fireEvent.click(await screen.findByRole("button", { name: "Confirm" }));
  await waitFor(() => {
    const call = fetcher.mock.calls.find(
      ([path, init]) =>
        path.endsWith("/assistance/settings") && init?.method === "POST",
    );
    expect(call).toBeDefined();
    expect(requestBody(call![1])).toEqual({ version: 4, overseer: true });
  });
});
