import { workspaceURL } from "./navigation";
import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { applyLiveEvent, nextCursor } from "./live-cache";
import { useScope } from "./data";

export function useLive(conversation: string) {
  const scope = useScope();
  const query = useQueryClient();
  const [status, setStatus] = useState("Connecting");
  useEffect(() => {
    let disposed = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let source: EventSource | undefined;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let delay = 3000;
    const key = "beepgrid:workspace:cursor:" + scope;
    let cursor = "";
    try {
      cursor = sessionStorage.getItem(key) || "";
    } catch {}
    function reconcile() {
      if (timer) return;
      timer = setTimeout(() => {
        timer = undefined;
        void query.invalidateQueries({
          predicate: (q) => q.queryKey[0] === scope,
        });
      }, 120);
    }
    function connect() {
      if (disposed) return;
      source = new EventSource(
        workspaceURL("/api/v1/events?stream=1&summary=1&after=") +
          encodeURIComponent(cursor) +
          (conversation
            ? "&conversation_id=" + encodeURIComponent(conversation)
            : ""),
      );
      source.onopen = () => {
        delay = 3000;
        setStatus("Live");
        reconcile();
      };
      source.onerror = () => {
        setStatus(navigator.onLine ? "Reconnecting" : "Offline");
        reconcile();
        // Native EventSource retries transient failures itself. A closed stream
        // needs a fresh connection after a proxy restart or terminal response.
        if (
          source?.readyState === EventSource.CLOSED &&
          !retry &&
          navigator.onLine
        ) {
          retry = setTimeout(() => {
            retry = undefined;
            source?.close();
            connect();
          }, delay);
          delay = Math.min(delay * 2, 30000);
        }
      };
      source.addEventListener("cursor", (e) => {
        cursor = nextCursor(cursor, (e as MessageEvent).lastEventId);
        try {
          sessionStorage.setItem(key, cursor);
        } catch {}
      });
      source.onmessage = (e) => {
        try {
          const event = JSON.parse(e.data);
          const previous = cursor;
          cursor = nextCursor(cursor, event.sequence || e.lastEventId);
          if (cursor === previous) return;
          try {
            sessionStorage.setItem(key, cursor);
          } catch {}
          applyLiveEvent(query, scope, conversation, event);
        } catch {
          reconcile();
        }
      };
    }
    connect();
    const safety = setInterval(reconcile, 15_000);
    const online = () => {
      setStatus(navigator.onLine ? "Reconnecting" : "Offline");
      reconcile();
      if (navigator.onLine && source?.readyState === EventSource.CLOSED) {
        clearTimeout(retry);
        retry = undefined;
        connect();
      }
    };
    window.addEventListener("online", online);
    window.addEventListener("offline", online);
    return () => {
      disposed = true;
      source?.close();
      clearTimeout(timer);
      clearTimeout(retry);
      clearInterval(safety);
      window.removeEventListener("online", online);
      window.removeEventListener("offline", online);
    };
  }, [scope, conversation, query]);
  return status;
}
