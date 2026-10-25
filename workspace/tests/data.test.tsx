import { renderHook, act, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { Session, useCollection, useOperation } from "../src/data";
import type { Me } from "../src/contracts";
const me = {
  company: "Acme",
  environment: "test",
  admin: false,
  person: {
    id: "p1",
    name: "Taylor",
    kind: "person",
    external_id: "taylor",
    metadata: {},
    disabled: false,
    created_at: "",
  },
} as Me;
function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <Session.Provider value={me}>{children}</Session.Provider>
    </QueryClientProvider>
  );
}
afterEach(() => vi.unstubAllGlobals());
it("continues across a zero-visible-row page with a cursor", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          items: [],
          next_cursor: "opaque",
          order: "id_ascending",
        }),
      ),
    )
    .mockResolvedValueOnce(
      new Response(
        JSON.stringify({ items: [{ id: "visible" }], order: "id_ascending" }),
      ),
    );
  vi.stubGlobal("fetch", fetcher);
  const { result } = renderHook(() => useCollection<{ id: string }>("agents"), {
    wrapper: wrapper(),
  });
  await waitFor(() => expect(result.current.hasNextPage).toBe(true));
  expect(result.current.items).toEqual([]);
  await act(() => result.current.fetchNextPage());
  await waitFor(() => expect(result.current.items).toEqual([{ id: "visible" }]));
  expect(JSON.parse(fetcher.mock.calls[1][1].body).cursor).toBe("opaque");
});
it("never converts a policy failure into an empty successful collection", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({
            error: { code: "POLICY_DENIED", message: "Not permitted" },
          }),
          { status: 403 },
        ),
      ),
  );
  const { result } = renderHook(() => useCollection("groups"), {
    wrapper: wrapper(),
  });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.error).toMatchObject({ status: 403 });
  expect(result.current.isSuccess).toBe(false);
});
it("fences repeated submits while one mutation is pending", async () => {
  let resolve!: (r: Response) => void;
  const fetcher = vi
    .fn()
    .mockImplementation(() => new Promise((r) => (resolve = r)));
  vi.stubGlobal("fetch", fetcher);
  const { result } = renderHook(() => useOperation(), { wrapper: wrapper() });
  let pending!: Promise<unknown>;
  act(() => {
    pending = result.current.run("/messages", { body: "one" });
  });
  await act(async () => {
    await result.current.run("/messages", { body: "one" });
  });
  expect(fetcher).toHaveBeenCalledTimes(1);
  await act(async () => {
    resolve(new Response(JSON.stringify({ ok: true })));
    await pending;
  });
  expect(result.current.pending).toBe(false);
});
