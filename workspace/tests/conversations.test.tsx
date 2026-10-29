import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import Conversations from "../src/pages/Conversations";
import { agent, harness, json, me } from "./fixtures";

afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  history.replaceState(null, "", "/agents");
});
it("retains a draft through a temporary read outage but clears it after authoritative contact loss", async () => {
  history.replaceState(null, "", "/conversations/con_one");
  let failure = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path.endsWith("/messages"))
        return json(
          { error: { code: "UNAVAILABLE", message: "Send result is unknown" } },
          503,
        );
      if (path.endsWith("/query"))
        return json({ items: [], order: "id_ascending" });
      if (failure)
        return json(
          {
            error: {
              code: failure === 404 ? "NOT_FOUND" : "UNAVAILABLE",
              message: "Temporarily unavailable",
            },
          },
          failure,
        );
      if (path.endsWith("/capabilities"))
        return json({
          operations: { view: true, message: true },
          outcomes: [],
        });
      return json({
        id: "con_one",
        title: "Private context",
        participants: [agent, me.person],
        messages: [],
      });
    }),
  );
  const { Wrapper, query } = harness();
  render(<Conversations />, { wrapper: Wrapper });
  const composer = await screen.findByRole("textbox", { name: "Message" });
  fireEvent.change(composer, { target: { value: "Keep my unsent work" } });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await screen.findByRole("alert");
  expect(
    Object.keys(sessionStorage).some(
      (k) =>
        k.includes(":operation:") &&
        sessionStorage.getItem(k)?.includes("Keep my unsent work"),
    ),
  ).toBe(true);
  failure = 503;
  await act(() => query.invalidateQueries());
  await screen.findByRole("alert");
  expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue(
    "Keep my unsent work",
  );
  failure = 0;
  await act(() => query.invalidateQueries());
  await waitFor(() =>
    expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue(
      "Keep my unsent work",
    ),
  );
  failure = 404;
  await act(() => query.invalidateQueries());
  await screen.findByRole("alert");
  await waitFor(() =>
    expect(
      Object.keys(sessionStorage).some((k) =>
        sessionStorage.getItem(k)?.includes("Keep my unsent work"),
      ),
    ).toBe(false),
  );
  failure = 0;
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  await waitFor(() =>
    expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue(""),
  );
});
