// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor, cleanup } from "@testing-library/react";
import { eventMessage, insertMessage, mergeMessages, useLiveConversation } from "./live";
import type { Conversation, EventItem, Message } from "./api";

const message = (id: string, at = "2026-10-20T10:00:00.123456Z"): Message => ({
  id, conversation_id: "con_one", sender_id: "agt_scout", instance_id: "ins_one",
  body: "<é>\ncanonical worker body", created_at: at,
});
const event = (sequence: number, m: Message = message("msg_one")): EventItem => ({
  sequence, kind: "message.created", actor_id: m.sender_id, conversation_id: "con_one",
  recipient_id: "", data: m, created_at: m.created_at,
});
const snapshot: Conversation = { id: "con_one", title: "One", created_by: "per_one", created_at: "2026-10-20T10:00:00Z", messages: [] };
const response = (value: unknown) => ({ ok: true, json: async () => value }) as Response;
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("canonical live messages", () => {
  it("requires matching event/conversation identity, complete content and attribution", () => {
    expect(eventMessage(event(1))).toEqual(message("msg_one"));
    expect(eventMessage({ ...event(1), data: null })).toBeNull();
    expect(eventMessage({ ...event(1), conversation_id: "foreign" })).toBeNull();
    expect(eventMessage(event(1, { ...message("bad"), instance_id: 3 } as never))).toBeNull();
    expect(eventMessage(event(1, { ...message("bad"), created_at: "bad" }))).toBeNull();
  });
  it("deduplicates replay and matches canonical sub-millisecond timestamp and ID ordering", () => {
    const values = [message("z", "2026-10-20T10:00:00.1Z"), message("a", "2026-10-20T10:00:00.100001Z"), message("b", "2026-10-20T10:00:00.100001Z"), message("zero", "2026-10-20T10:00:00Z")];
    expect(mergeMessages(values, [values[1]]).map(m => m.id)).toEqual(["zero", "z", "a", "b"]);
    const many = Array.from({ length: 650 }, (_, i) => message(String(i).padStart(4, "0")));
    const bounded = mergeMessages(many.slice(0, 300), many.slice(200));
    expect(bounded).toHaveLength(500);
    expect(bounded[0].id).toBe("0150");
    expect(bounded.at(-1)?.id).toBe("0649");
  });
  it("matches canonical history after shuffled arrivals, replay and a full window", () => {
    const all = Array.from({ length: 700 }, (_, i) => message(String(i).padStart(4, "0"), `2026-10-20T10:00:00.${String(i % 25).padStart(6, "0")}Z`));
    const arrivals = all.filter((_, i) => i % 2).reverse().concat(all.filter((_, i) => !(i % 2)), all.slice(50, 100));
    let values: Message[] = [];
    for (const m of arrivals) values = insertMessage(values, m);
    expect(values).toEqual(mergeMessages(all));
    expect(insertMessage(values, values[0])).toBe(values);
    expect(insertMessage(values, message("old", "2026-10-19T10:00:00Z"))).toBe(values);
  });
});

describe("snapshot and stream races", () => {
  it("retains a message delivered while the initial history read is pending", async () => {
    let complete!: (value: Response) => void;
    vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>(resolve => { complete = resolve; })));
    const onError = vi.fn();
    const { result } = renderHook(() => useLiveConversation("con_one", 0, onError));
    act(() => result.current.acceptMessage(message("new")));
    await act(async () => complete(response(snapshot)));
    await waitFor(() => expect(result.current.conversation?.messages).toEqual([message("new")]));
    act(() => result.current.acceptMessage(message("new")));
    expect(result.current.conversation?.messages).toHaveLength(1);
    expect(onError).not.toHaveBeenCalled();
  });
  it("merges against an older reconciliation snapshot rather than losing new messages", async () => {
    let complete!: (value: Response) => void;
    const fetcher = vi.fn().mockResolvedValueOnce(response(snapshot)).mockImplementationOnce(() => new Promise<Response>(resolve => { complete = resolve; }));
    vi.stubGlobal("fetch", fetcher);
    const onError = vi.fn();
    const { result, rerender } = renderHook(({ revision }) => useLiveConversation("con_one", revision, onError), { initialProps: { revision: 0 } });
    await waitFor(() => expect(result.current.conversation?.id).toBe("con_one"));
    rerender({ revision: 1 });
    act(() => result.current.acceptMessage(message("during-refresh")));
    await act(async () => complete(response(snapshot)));
    expect(result.current.conversation?.messages?.map(m => m.id)).toEqual(["during-refresh"]);
  });
  it("ignores a late old-route response and clears denied content on reconciliation", async () => {
    let complete!: (value: Response) => void;
    vi.stubGlobal("fetch", vi.fn().mockImplementationOnce(() => new Promise<Response>(resolve => { complete = resolve; }))
      .mockResolvedValueOnce(response({ ...snapshot, id: "con_two", messages: [message("two")] }))
      .mockResolvedValueOnce({ ok: false, status: 404, json: async () => ({ error: { message: "Conversation unavailable" } }) }));
    const onError = vi.fn();
    const { result, rerender } = renderHook(({ id, revision }) => useLiveConversation(id, revision, onError), { initialProps: { id: "con_one", revision: 0 } });
    act(() => result.current.acceptMessage(message("old")));
    rerender({ id: "con_two", revision: 0 });
    await waitFor(() => expect(result.current.conversation?.id).toBe("con_two"));
    await act(async () => complete(response({ ...snapshot, messages: [message("old")] })));
    expect(result.current.conversation?.id).toBe("con_two");
    rerender({ id: "con_two", revision: 1 });
    await waitFor(() => expect(result.current.conversation).toBeNull());
    expect(onError).toHaveBeenCalledWith("Conversation unavailable");
  });
});
