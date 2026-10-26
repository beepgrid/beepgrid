import { render, screen, act } from "@testing-library/react";
import { afterEach, describe, it, expect, vi } from "vitest";
import {
  configureWorkspaceBase,
  navigate,
  useRoute,
  workspaceURL,
} from "../src/navigation";
import { api } from "../src/client";
afterEach(() => {
  configureWorkspaceBase("");
  history.replaceState(null, "", "/");
  vi.unstubAllGlobals();
});
describe("trusted host composition", () => {
  it("keeps route parsing and links within the configured workspace", () => {
    configureWorkspaceBase("/w/tenant_a");
    history.replaceState(null, "", "/w/tenant_a/agents/agent_a");
    function Route() {
      const route = useRoute();
      return (
        <span>
          {route.page}/{route.id}
        </span>
      );
    }
    render(<Route />);
    expect(screen.getByText("agents/agent_a")).toBeInTheDocument();
    act(() => navigate("/conversations/thread_a"));
    expect(location.pathname).toBe("/w/tenant_a/conversations/thread_a");
    expect(screen.getByText("conversations/thread_a")).toBeInTheDocument();
    expect(workspaceURL("/api/v1/events")).toBe("/w/tenant_a/api/v1/events");
  });
  it("binds API calls to the same host path without adding credentials", async () => {
    configureWorkspaceBase("/w/tenant_a");
    const fetch = vi
      .fn()
      .mockResolvedValue({ ok: true, json: async () => ({ ok: true }) });
    vi.stubGlobal("fetch", fetch);
    await api("/query", { collection: "agents" });
    expect(fetch.mock.calls[0][0]).toBe("/w/tenant_a/api/v1/query");
    expect(fetch.mock.calls[0][1].headers.Authorization).toBeUndefined();
  });
  it("rejects malformed host bases", () => {
    for (const base of ["//evil", "/w/tenant_a/../tenant_b", "/x/tenant_a"])
      expect(() => configureWorkspaceBase(base)).toThrow();
  });
});
