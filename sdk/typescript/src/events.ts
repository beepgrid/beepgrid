import { NotifError } from "./index.js";

export type NotifEvent = {
  sequence: number;
  kind: string;
  actor_id: string;
  conversation_id: string;
  recipient_id: string;
  data: unknown;
  created_at: string;
};
export type EventOptions = { after?: number; signal?: AbortSignal; summary?: boolean };

// The server bounds event payloads through its request limits. A finite parser
// budget also prevents a broken intermediary from retaining an unbounded frame.
const MAX_FRAME = 8 * 1024 * 1024;
export async function* readEvents(base: string, token: string, transport: typeof fetch, options: EventOptions): AsyncGenerator<NotifEvent> {
  let cursor = options.after ?? 0;
  if (!Number.isSafeInteger(cursor) || cursor < 0) throw new Error("Invalid event cursor");
  const control = new AbortController();
  const abort = () => control.abort(options.signal?.reason);
  options.signal?.addEventListener("abort", abort, { once: true });
  if (options.signal?.aborted) abort();
  let backoff = 100;
  try {
    while (!control.signal.aborted) {
      let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
      let received = false;
      try {
        const response = await transport(`${base}/api/v1/events?stream=1&after=${cursor}${options.summary ? "&summary=1" : ""}`, {
          redirect: "error", headers: { Authorization: `Bearer ${token}`, "Last-Event-ID": String(cursor) }, signal: control.signal,
        });
        if (!response.ok) {
          // Do not retry revoked credentials or a policy denial indefinitely.
          if (response.status < 500 && response.status !== 429) {
            await response.body?.cancel();
            throw new NotifError("EVENT_STREAM_REJECTED", "Event stream rejected", response.status);
          }
          await response.body?.cancel();
          throw new Error("Event stream temporarily unavailable");
        }
        if (!response.body || !response.headers.get("Content-Type")?.includes("text/event-stream")) {
          await response.body?.cancel();
          throw new NotifError("INVALID_EVENT_STREAM", "Expected an event stream", 502);
        }
        reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "", bytes = 0, data: string[] = [], id = "", kind = "";
        for (;;) {
          const chunk = await reader.read();
          if (chunk.done) break; // Never commit an unterminated frame after a disconnect.
          buffer += decoder.decode(chunk.value, { stream: true });
          let end;
          while ((end = buffer.indexOf("\n")) >= 0) {
            let line = buffer.slice(0, end); buffer = buffer.slice(end + 1);
            if (line.endsWith("\r")) line = line.slice(0, -1);
            bytes += line.length + 1;
            if (bytes > MAX_FRAME) throw new NotifError("INVALID_EVENT_STREAM", "Event frame exceeds limit", 502);
            if (line === "") {
              if (id && data.length) {
                const sequence = Number(id);
                if (!Number.isSafeInteger(sequence) || sequence < 0) throw new NotifError("INVALID_EVENT_STREAM", "Invalid event ID", 502);
                if (sequence > cursor) {
                  if (kind === "cursor") { cursor = sequence; }
                  else {
                    let event: NotifEvent;
                    try { event = JSON.parse(data.join("\n")); } catch { throw new NotifError("INVALID_EVENT_STREAM", "Invalid event JSON", 502); }
                    if (event.sequence !== sequence || typeof event.kind !== "string") throw new NotifError("INVALID_EVENT_STREAM", "Event identity mismatch", 502);
                    cursor = sequence;
                    received = true;
                    backoff = 100;
                    yield event;
                  }
                }
              }
              bytes = 0; data = []; id = ""; kind = "";
            } else if (!line.startsWith(":")) {
              const colon = line.indexOf(":"), name = colon < 0 ? line : line.slice(0, colon);
              let value = colon < 0 ? "" : line.slice(colon + 1);
              if (value.startsWith(" ")) value = value.slice(1);
              if (name === "data") data.push(value);
              else if (name === "id") id = value;
              else if (name === "event") kind = value;
            }
          }
          if (buffer.length + bytes > MAX_FRAME) throw new NotifError("INVALID_EVENT_STREAM", "Event frame exceeds limit", 502);
        }
      } catch (error) {
        if (control.signal.aborted) return;
        if (error instanceof NotifError) throw error;
        // Connection loss is retried from the last completely consumed event.
      } finally {
        if (reader) { try { await reader.cancel(); } catch {} reader.releaseLock(); }
      }
      if (!received) {
        await pause(backoff, control.signal);
        backoff = Math.min(backoff * 2, 2000);
      }
    }
  } finally {
    control.abort();
    options.signal?.removeEventListener("abort", abort);
  }
}
function pause(ms: number, signal: AbortSignal) {
  return new Promise<void>(resolve => {
    if (signal.aborted) { resolve(); return; }
    const done = () => { clearTimeout(timer); signal.removeEventListener("abort", done); resolve(); };
    const timer = setTimeout(done, ms);
    signal.addEventListener("abort", done, { once: true });
  });
}
