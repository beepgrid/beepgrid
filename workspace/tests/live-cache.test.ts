import { describe, it, expect, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { applyLiveEvent, nextCursor } from "../src/live-cache";
import { groupQuestions } from "../src/request-groups";
import type { Request, Conversation } from "../src/contracts";
describe("durable event cache", () => {
  it("only advances safe monotonic cursors, including gaps hidden by policy", () => {
    expect(nextCursor("10", 11)).toBe("11");
    expect(nextCursor("10", 45)).toBe("45");
    for (const value of [
      10,
      9,
      -1,
      "invalid",
      1.5,
      Number.MAX_SAFE_INTEGER + 1,
    ])
      expect(nextCursor("10", value)).toBe("10");
  });
  it("appends a canonical selected message once without touching another scope", () => {
    const q = new QueryClient();
    const key = ["scope", "resource", "/conversations/c"];
    q.setQueryData(key, { id: "c", messages: [] } as unknown as Conversation);
    q.setQueryData(["other", "resource", "/conversations/c"], {
      id: "c",
      messages: [],
    });
    const event = {
      kind: "message.created",
      conversation_id: "c",
      data: {
        id: "m",
        conversation_id: "c",
        body: "<script>untrusted</script>",
        created_at: "2026-10-25T00:00:00Z",
      },
    };
    applyLiveEvent(q, "scope", "c", event);
    applyLiveEvent(q, "scope", "c", event);
    expect(q.getQueryData<Conversation>(key)?.messages).toHaveLength(1);
    expect(
      q.getQueryData<Conversation>(["other", "resource", "/conversations/c"])
        ?.messages,
    ).toHaveLength(0);
  });
  it("worker traffic does not invalidate conversations; permission changes remove stale content", () => {
    const q = new QueryClient();
    const key = ["scope", "resource", "/conversations/c"];
    q.setQueryData(key, { id: "c", messages: [{ id: "secret" }] });
    applyLiveEvent(q, "scope", "c", { kind: "worker.received" });
    expect(q.getQueryState(key)?.isInvalidated).toBe(false);
    applyLiveEvent(q, "scope", "c", { kind: "group.saved" });
    expect(q.getQueryData(key)).toBeUndefined();
  });
});
it("groups only exact compatible ordinary questions, preserving independent IDs", () => {
  const base = {
    kind: "question",
    state: "open",
    shared_key: "exact/v1",
    title: "Tax?",
    body: "Context",
    choices: ["Yes", "No"],
  } as Request;
  const items = [
    { ...base, id: "a" },
    { ...base, id: "b" },
    { ...base, id: "c", choices: ["No", "Yes"] },
    { ...base, id: "d", shared_key: "" },
    { ...base, id: "e", kind: "approval" },
    { ...base, id: "f", state: "resolved" },
  ] as Request[];
  expect(groupQuestions(items).map((g) => g.map((r) => r.id))).toEqual([
    ["a", "b"],
    ["c"],
    ["d"],
    ["e"],
    ["f"],
  ]);
});

it("resets the workspace cache on a targeted shared-chat access change without a message body", async () => {
  const query = new QueryClient();
  query.setQueryData(["test", "resource", "/conversations/con_removed"], {
    id: "con_removed",
    messages: [{ body: "Previously visible" }],
  });
  query.setQueryData(["other", "resource", "/conversations/con_kept"], {
    id: "con_kept",
  });
  applyLiveEvent(query, "test", "con_removed", {
    kind: "conversation.access_changed",
    sequence: 50,
  });
  expect(
    query.getQueryData(["test", "resource", "/conversations/con_removed"]),
  ).toBeUndefined();
  expect(
    query.getQueryData(["other", "resource", "/conversations/con_kept"]),
  ).toEqual({ id: "con_kept" });
});
