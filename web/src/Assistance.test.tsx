// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import {
  render,
  screen,
  fireEvent,
  cleanup,
  waitFor,
} from "@testing-library/react";
import { afterEach, it, expect, vi } from "vitest";
import { AnswerSuggestions, FleetSummary } from "./Assistance";
import { type RequestItem } from "./api";
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
it("a semantic suggestion requires explicit scope review before resolving", async () => {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      calls.push(path);
      return {
        ok: true,
        json: async () =>
          path.includes("suggestions")
            ? {
                matcher_configured: true,
                proposals: [
                  {
                    id: "p",
                    reason: "Review resource version",
                    answer: {
                      id: "a",
                      key: "policy/v1",
                      question: "Tax?",
                      answer: "Include tax",
                      valid_until: "2027-01-01T00:00:00Z",
                    },
                  },
                ],
              }
            : {},
      };
    }),
  );
  const changed = vi.fn();
  render(
    <AnswerSuggestions
      request={{ id: "q", state: "open", kind: "question" } as RequestItem}
      onChanged={changed}
    />,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Find a related answer" }),
  );
  expect(await screen.findByText("Include tax")).toBeVisible();
  expect(changed).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("button", { name: "Scope matches, use answer" }),
  );
  await waitFor(() => expect(changed).toHaveBeenCalledTimes(1));
  expect(calls).toContain("/api/v1/answer-proposals/p");
});
it("reports missing semantic configuration without blocking the ordinary request", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: true,
      json: async () => ({ matcher_configured: false, proposals: [] }),
    })),
  );
  render(
    <AnswerSuggestions
      request={{ id: "q" } as RequestItem}
      onChanged={() => {}}
    />,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Find a related answer" }),
  );
  expect(await screen.findByRole("status")).toHaveTextContent("not configured");
});
it("fleet coverage shows stale agents and never invents aggregated progress", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: true,
      json: async () => ({
        summary: "1 of 2 permitted agents recently connected.",
        as_of: "2026-10-20T08:00:00Z",
        agents: [
          {
            agent: { id: "a", name: "Scout" },
            fresh: true,
            instances: [{ progress: { processed: 12 } }],
          },
          { agent: { id: "b", name: "Builder" }, fresh: false, instances: [] },
        ],
      }),
    })),
  );
  render(<FleetSummary />);
  fireEvent.click(screen.getByRole("button", { name: "Ask for fleet status" }));
  expect(await screen.findByText("No fresh connection")).toBeVisible();
  expect(screen.getByText("Scout")).toBeVisible();
  expect(screen.getByText('{"processed":12}')).toBeVisible();
});
