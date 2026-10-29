import { act, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useLive } from "../src/live";
import { harness } from "./fixtures";
class Stream {
  static CLOSED = 2;
  static CONNECTING = 0;
  static all: Stream[] = [];
  readyState = 0;
  onopen?: () => void;
  onerror?: () => void;
  onmessage?: () => void;
  constructor(public url: string) {
    Stream.all.push(this);
  }
  addEventListener() {}
  close() {
    this.readyState = 2;
  }
}
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  Stream.all = [];
  sessionStorage.clear();
});
function View() {
  return <span>{useLive("")}</span>;
}
it("recovers a closed stream with backoff but leaves native transient reconnects alone", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("EventSource", Stream);
  const { Wrapper } = harness();
  const view = render(<View />, { wrapper: Wrapper });
  const first = Stream.all[0];
  act(() => {
    first.onerror?.();
  });
  await act(() => vi.advanceTimersByTimeAsync(4000));
  expect(Stream.all).toHaveLength(1);
  act(() => {
    first.readyState = 2;
    first.onerror?.();
  });
  await act(() => vi.advanceTimersByTimeAsync(3000));
  expect(Stream.all).toHaveLength(2);
  act(() => Stream.all[1].onopen?.());
  expect(screen.getByText("Live")).toBeInTheDocument();
  act(() => {
    Stream.all[1].readyState = 2;
    Stream.all[1].onerror?.();
  });
  view.unmount();
  await act(() => vi.advanceTimersByTimeAsync(30000));
  expect(Stream.all).toHaveLength(2);
});
