import { renderHook, act, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { PageLoader, usePage, type PageComponent } from "../src/route-loader";
it("a late route load cannot replace the section selected afterward", async () => {
  let resolve!: (value: { default: PageComponent }) => void;
  const first = () => <p>First</p>;
  const second = () => <p>Second</p>;
  const loader = new PageLoader({
    first: () => new Promise((r) => (resolve = r)),
    second: async () => ({ default: second }),
  });
  const { result, rerender } = renderHook(({ page }) => usePage(loader, page), {
    initialProps: { page: "first" },
  });
  rerender({ page: "second" });
  await waitFor(() => expect(result.current.component).toBe(second));
  await act(async () => resolve({ default: first }));
  expect(result.current.component).toBe(second);
});
it("allows an explicit chunk-load retry and reuses successful code without a loading delay", async () => {
  const page = () => <p>Loaded</p>;
  const load = vi
    .fn()
    .mockRejectedValueOnce(Error("Offline"))
    .mockResolvedValue({ default: page });
  const loader = new PageLoader({ page: load });
  const { result } = renderHook(() => usePage(loader, "page"));
  await waitFor(() => expect(result.current.error?.message).toBe("Offline"));
  act(() => result.current.retry());
  await waitFor(() => expect(result.current.component).toBe(page));
  const second = renderHook(() => usePage(loader, "page"));
  expect(second.result.current.component).toBe(page);
  expect(load).toHaveBeenCalledTimes(2);
});
