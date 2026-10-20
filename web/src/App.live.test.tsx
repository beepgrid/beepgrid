// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { App } from "./App";

const person = { id: "per_owner", kind: "person", name: "Alex", external_id: "alex", metadata: {}, disabled: false, created_at: "2026-10-20T08:00:00Z" };
const agent = { ...person, id: "agt_scout", kind: "agent", name: "Scout", external_id: "research/scout" };
const one = { id: "con_one", title: "Research chat", created_by: person.id, created_at: person.created_at };
const two = { ...one, id: "con_two", title: "Other chat" };
const msg = (id = "msg_new", conversation_id = one.id) => ({ id, conversation_id, sender_id: agent.id, instance_id: "ins_scout", body: "Canonical live <é> reply " + id, created_at: person.created_at });
class Source {
  static all: Source[] = [];
  onmessage?: (frame: MessageEvent) => void;
  onopen?: () => void;
  onerror?: () => void;
  closed = false;
  listeners = new Map<string, (e: MessageEvent) => void>();
  constructor(public url: string) { Source.all.push(this); }
  addEventListener(kind: string, fn: (e: MessageEvent) => void) { this.listeners.set(kind, fn); }
  close() { this.closed = true; }
  emit(kind: string, sequence: number, data: unknown = null, conversation_id = one.id) {
    this.onmessage?.({ data: JSON.stringify({ sequence, kind, data, conversation_id, actor_id: agent.id, created_at: person.created_at }), lastEventId: String(sequence) } as MessageEvent);
  }
}
let inboxRequests: unknown[];
let calls: string[], messages: ReturnType<typeof msg>[], files: { id: string; file_name: string; size: number }[], denied: boolean, expired: boolean;
const latest = () => Source.all.at(-1)!;
beforeEach(() => {
  vi.useFakeTimers();
  history.replaceState(null, "", "/conversations/con_one");
  Source.all = []; calls = []; messages = []; inboxRequests = []; files = []; denied = false; expired = false;
  vi.stubGlobal("EventSource", Source);
  Element.prototype.scrollIntoView = function () {};
  vi.stubGlobal("fetch", vi.fn(async (input: string, init?: RequestInit) => {
    const path = String(input).replace("/api/v1", ""); calls.push(path);
    let value: unknown = [];
    if ((path === "/me" && expired) || (path === "/conversations/con_one" && denied)) return { ok: false, status: expired ? 401 : 404, json: async () => ({ error: { message: "Conversation unavailable" } }) } as Response;
    if (path === "/me") value = { person, admin: true, company: "Acme", environment: "test" };
    else if (path === "/people") value = [person];
    else if (path === "/requests") value = inboxRequests;
    else if (path === "/agents") value = [agent];
    else if (path === "/conversations") value = [one, two];
    else if (path === "/conversations/con_one") value = { ...one, participants: [person, agent], messages: [...messages] };
    else if (path === "/conversations/con_two") value = { ...two, participants: [person, agent], messages: [] };
    else if (path.endsWith("/attachments")) value = [...files];
    else if (path === "/messages") {
      const body = JSON.parse(String(init?.body)).body;
      const sent = { ...msg("sent"), body, sender_id: person.id };
      messages.push(sent); latest().emit("message.created", 101, sent);
      value = { accepted: true };
    }
    return { ok: true, status: 200, json: async () => value } as Response;
  }));
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });
async function open() {
  await act(async () => { render(<App />); });
  screen.getByRole("textbox", { name: "Message" });
  expect(Source.all.length).toBeGreaterThan(0);
  act(() => latest().onopen?.());
  calls.length = 0;
}
async function advance(ms: number) { await act(async () => { await vi.advanceTimersByTimeAsync(ms); }); }

describe("dashboard live path", () => {
  it("appends and deduplicates canonical replies without history or workspace refetch", async () => {
    await open();
    expect(latest().url).toContain("summary=1");
    expect(latest().url).toContain("conversation_id=con_one");
    act(() => { latest().emit("message.created", 1, msg()); latest().emit("message.created", 1, msg()); latest().emit("message.created", 2, msg()); });
    expect(screen.getAllByText(msg().body)).toHaveLength(1);
    expect(screen.getByText("worker")).toBeVisible();
    await advance(2200);
    expect(calls).toEqual([]);
  });
  it("consumes its own compact send through events without discarding a draft early or refetching", async () => {
    await open();
    const input = screen.getByRole("textbox", { name: "Message" });
    fireEvent.change(input, { target: { value: "My saved message" } });
    await act(async () => fireEvent.submit(input.closest("form")!));
    expect(screen.getByText("My saved message")).toBeVisible();
    expect(input).toHaveValue("");
    await advance(2200);
    expect(calls).toEqual(["/messages"]);
  });
  it("refreshes an acknowledged own send promptly when the event connection is reconnecting", async () => {
    await open();
    act(() => { latest().onerror?.(); latest().onmessage = undefined; });
    const input = screen.getByRole("textbox", { name: "Message" });
    fireEvent.change(input, { target: { value: "Sent during reconnection" } });
    await act(async () => fireEvent.submit(input.closest("form")!));
    expect(screen.getByText("Sent during reconnection")).toBeVisible();
    expect(input).toHaveValue("");
    expect(calls).toContain("/conversations/con_one");
  });
  it("preserves older-server fallback and authoritative permission reconciliation", async () => {
    await open();
    messages.push(msg("fallback"));
    act(() => latest().emit("message.created", 1));
    await advance(2100);
    expect(screen.getByText(msg("fallback").body)).toBeVisible();
    denied = true;
    act(() => latest().emit("role.binding_changed", 2));
    await advance(2100);
    expect(screen.queryByText(msg("fallback").body)).toBeNull();
    expect(screen.getByText("Conversation unavailable")).toBeVisible();
  });
  it("reconciles missed events and expired browser sessions on the original 15-second cadence", async () => {
    await open();
    messages.push(msg("missed"));
    await advance(15000);
    expect(screen.getByText(msg("missed").body)).toBeVisible();
    expired = true;
    await advance(15000);
    expect(screen.getByRole("heading", { name: "Welcome to Notif" })).toBeVisible();
    expect(Source.all.every(s => s.closed)).toBe(true);
    expect(screen.queryByText(msg("missed").body)).toBeNull();
  });
  it("keeps replay cursors through navigation and does not postpone session checks", async () => {
    await open();
    act(() => latest().emit("message.created", 6, msg()));
    act(() => latest().listeners.get("cursor")?.({ lastEventId: "10" } as MessageEvent));
    await advance(10000);
    const previous = latest();
    fireEvent.click(screen.getByRole("button", { name: /Other chat/ }));
    await act(async () => {});
    expect(previous.closed).toBe(true);
    expect(latest().url).toContain("after=10");
    expect(latest().url).toContain("conversation_id=con_two");
    act(() => previous.emit("message.created", 50, msg("stale")));
    expect(screen.queryByText(msg("stale").body)).toBeNull();
    expired = true;
    await advance(5000);
    expect(screen.getByRole("heading", { name: "Welcome to Notif" })).toBeVisible();
  });
  it("keeps background messages lightweight and avoids refetching agent instance details", async () => {
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Agents" }));
    fireEvent.click(screen.getByText("research/scout").closest("button")!);
    await act(async () => {});
    expect(calls.some(p => p.endsWith("/instances"))).toBe(true);
    calls.length = 0;
    expect(latest().url).not.toContain("conversation_id=");
    act(() => { for (let i = 1; i <= 25; i++) { latest().emit("message.created", i * 2, null, two.id); latest().emit("worker.received", i * 2 + 1); } });
    await advance(2200);
    expect(calls).toEqual([]);
  });
  it("refreshes attachments from their events without downloading the chat again", async () => {
    await open();
    files.push({ id: "file_one", file_name: "report.pdf", size: 120 });
    act(() => latest().emit("attachment.created", 1));
    await act(async () => {});
    expect(screen.getByRole("link", { name: /report.pdf/ })).toBeVisible();
    expect(calls).toEqual(["/conversations/con_one/attachments"]);
  });
  it("keeps the full history window moving and scrolls to a new last message", async () => {
    messages = Array.from({ length: 500 }, (_, i) => msg("seed_" + String(i).padStart(4, "0")));
    await open();
    const scroll = vi.spyOn(Element.prototype, "scrollIntoView");
    scroll.mockClear();
    const next = { ...msg("latest"), created_at: "2026-10-20T08:01:00Z" };
    act(() => latest().emit("message.created", 1, next));
    expect(screen.getByText(next.body)).toBeVisible();
    expect(screen.queryByText(msg("seed_0000").body)).toBeNull();
    expect(document.querySelectorAll(".chat-history article")).toHaveLength(500);
    expect(scroll).toHaveBeenCalledWith({ block: "nearest" });
    scroll.mockRestore();
  });
  it("updates memoized message authors after an authoritative directory change", async () => {
    messages = [msg("existing")];
    await open();
    const original = vi.mocked(fetch).getMockImplementation()!;
    vi.mocked(fetch).mockImplementation((input, init) => String(input).endsWith("/agents")
      ? Promise.resolve({ ok: true, json: async () => [{ ...agent, name: "Renamed Scout" }] } as Response)
      : original(input, init));
    act(() => latest().emit("principal.updated", 1));
    await advance(2100);
    expect(screen.getByText("Renamed Scout")).toBeVisible();
    expect(screen.getByText(msg("existing").body)).toBeVisible();
  });
  it("keeps pending request invalidation alive when navigating away from a chat", async () => {
    await open();
    inboxRequests.push({ id: "req_new", requester_id: agent.id, conversation_id: one.id, kind: "question", title: "Do we proceed?", body: "Keep this question", choices: ["Continue", "Wait"], state: "open", version: 1, created_at: person.created_at, expires_at: "2026-11-20T10:00:00Z" });
    act(() => latest().emit("request.created", 1));
    fireEvent.click(screen.getByRole("button", { name: "Inbox" }));
    await advance(2100);
    expect(screen.getByRole("button", { name: /Do we proceed/ })).toBeVisible();
  });
  it("refreshes delivery state even when the activity window is already full", async () => {
    await open();
    const original = vi.mocked(fetch).getMockImplementation()!;
    vi.mocked(fetch).mockImplementation((input, init) => String(input).endsWith("/activity")
      ? Promise.resolve({ ok: true, json: async () => Array.from({ length: 200 }, (_, i) => ({ sequence: 200-i, kind: "message.created", actor_id: agent.id, data: null, created_at: person.created_at })) } as Response)
      : original(input, init));
    fireEvent.click(screen.getByRole("button", { name: "Activity" }));
    await act(async () => {});
    fireEvent.click(screen.getByRole("button", { name: "Deliveries & recovery" }));
    await act(async () => {});
    calls.length = 0;
    act(() => latest().emit("worker.received", 201));
    await advance(2100);
    expect(calls.filter(p => p === "/deliveries")).toHaveLength(1);
  });
});
