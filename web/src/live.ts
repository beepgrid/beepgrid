import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Conversation, type EventItem, type Message } from "./api";

export function eventMessage(event: EventItem): Message | null {
  if (event.kind !== "message.created" || !event.data || typeof event.data !== "object") return null;
  const m = event.data as Record<string, unknown>;
  if (typeof m.id !== "string" || !m.id || typeof m.body !== "string" ||
      typeof m.sender_id !== "string" || typeof m.created_at !== "string" ||
      !Number.isFinite(Date.parse(m.created_at)) || !event.conversation_id ||
      m.conversation_id !== event.conversation_id ||
      (m.instance_id !== undefined && typeof m.instance_id !== "string")) return null;
  return m as Message;
}

// Match the canonical last-500 window and timestamp/ID ordering. PostgreSQL can
// return more timestamp precision than Date.parse preserves, including ties
// within one millisecond, so normalize the UTC fractional seconds before sorting.
function timeKey(value: string) {
  return value.replace(/(\.\d+)?Z$/, (_, fraction: string | undefined) =>
    (fraction ?? ".").padEnd(10, "0") + "Z");
}
export function mergeMessages(messages: Message[] = [], pending: Message[] = []): Message[] {
  const byID = new Map(messages.map(m => [m.id, m]));
  for (const m of pending) byID.set(m.id, m);
  return [...byID.values()].sort((a, b) => {
    const at = timeKey(a.created_at), bt = timeKey(b.created_at);
    return at < bt ? -1 : at > bt ? 1 : a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  }).slice(-500);
}

// The existing window is already ordered. Insert one immutable event without
// sorting and allocating timestamp keys for every old message on every reply.
export function insertMessage(messages: Message[] = [], message: Message): Message[] {
  if (messages.some(m => m.id === message.id)) return messages;
  const at = timeKey(message.created_at);
  let lo = 0, hi = messages.length;
  while (lo < hi) {
    const mid = (lo + hi) >>> 1, other = messages[mid], bt = timeKey(other.created_at);
    if (bt < at || (bt === at && other.id < message.id)) lo = mid + 1;
    else hi = mid;
  }
  if (lo === 0 && messages.length >= 500) return messages;
  const next = messages.slice();
  next.splice(lo, 0, message);
  if (next.length > 500) next.shift();
  return next;
}

// A snapshot and its stream may complete in either order. Retain only messages
// received during this read, and merge them after the snapshot arrives. An old
// route, failed authorization check or aborted read cannot publish stale data.
export function useLiveConversation(id: string | null, revision: number, onError: (message: string) => void) {
  const [conversation, setConversation] = useState<Conversation | null>(null);
  const loading = useRef<{ id: string; pending: Message[] } | null>(null);
  const acceptMessage = useCallback((message: Message) => {
    const current = loading.current;
    if (current && current.id === message.conversation_id)
      current.pending = insertMessage(current.pending, message);
    setConversation(old => {
      if (!old || old.id !== message.conversation_id) return old;
      const messages = insertMessage(old.messages, message);
      return messages === old.messages ? old : { ...old, messages };
    });
  }, []);
  useEffect(() => {
    const abort = new AbortController();
    const read = id ? { id, pending: [] as Message[] } : null;
    loading.current = read;
    setConversation(old => old?.id === id ? old : null);
    if (id) void api<Conversation>("/conversations/" + encodeURIComponent(id), undefined, { signal: abort.signal })
      .then(snapshot => {
        if (abort.signal.aborted || loading.current !== read) return;
        setConversation({ ...snapshot, messages: mergeMessages(snapshot.messages, read!.pending) });
        loading.current = null;
      })
      .catch(e => {
        if (abort.signal.aborted || loading.current !== read) return;
        loading.current = null;
        setConversation(null);
        onError(e.message);
      });
    return () => { abort.abort(); if (loading.current === read) loading.current = null; };
  }, [id, revision, onError]);
  return { conversation, setConversation, acceptMessage };
}
