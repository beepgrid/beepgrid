import { afterEach, describe, expect, it, vi } from "vitest";
import { api, APIError, bytes, Operation } from "../src/client";
afterEach(() => vi.unstubAllGlobals());
function response(status: number, value: unknown) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
describe("operation identity", () => {
  it("reuses the exact key after ambiguous failure and changes it with content", async () => {
    const fetcher = vi
      .fn()
      .mockRejectedValueOnce(Error("Connection lost"))
      .mockResolvedValue(response(200, { id: "m1" }));
    vi.stubGlobal("fetch", fetcher);
    const op = new Operation();
    await expect(op.send("/messages", { body: "hello" })).rejects.toThrow(
      "Connection lost",
    );
    await op.send("/messages", { body: "hello" });
    const first = fetcher.mock.calls[0][1],
      second = fetcher.mock.calls[1][1];
    expect(first.headers["Idempotency-Key"]).toBe(
      second.headers["Idempotency-Key"],
    );
    fetcher.mockResolvedValue(response(200, { id: "m2" }));
    await op.send("/messages", { body: "changed" });
    expect(fetcher.mock.calls[2][1].headers["Idempotency-Key"]).not.toBe(
      first.headers["Idempotency-Key"],
    );
  });
  it("creates independent identities after confirmed acceptance", async () => {
    const fetcher = vi
      .fn()
      .mockImplementation(() => Promise.resolve(response(200, {})));
    vi.stubGlobal("fetch", fetcher);
    const op = new Operation();
    await op.send("/messages", { body: "same" });
    op.accepted();
    await op.send("/messages", { body: "same" });
    expect(fetcher.mock.calls[0][1].headers["Idempotency-Key"]).not.toBe(
      fetcher.mock.calls[1][1].headers["Idempotency-Key"],
    );
  });
  it("preserves structured permission errors and UTF-8 limits", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          response(403, {
            error: {
              code: "POLICY_DENIED",
              message: "This operation is not permitted.",
            },
          }),
        ),
    );
    await expect(api("/groups")).rejects.toMatchObject({
      status: 403,
      code: "POLICY_DENIED",
    });
    expect(bytes("😀")).toBe(4);
  });
});
