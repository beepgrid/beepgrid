import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useScope } from "./data";

export function useLive(conversation: string) {
  const scope = useScope();
  const query = useQueryClient();
  const [status, setStatus] = useState("Connecting");
  useEffect(() => {
    let disposed = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let source: EventSource | undefined;
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
        "/api/v1/events?stream=1&summary=1&after=" +
          encodeURIComponent(cursor) +
          (conversation
            ? "&conversation_id=" + encodeURIComponent(conversation)
            : ""),
      );
      source.onopen = () => {
        setStatus("Live");
        reconcile();
      };
      source.onerror = () => {
        setStatus(navigator.onLine ? "Reconnecting" : "Offline");
      };
      source.onmessage = (e) => {
        try {
          const event = JSON.parse(e.data);
          const seq = String(event.sequence || e.lastEventId || "");
          if (seq) {
            cursor = seq;
            try {
              sessionStorage.setItem(key, seq);
            } catch {}
          }
          // Invalidation is deliberately authoritative. The backend filters every replay.
          // Remove cached private content on policy changes before fetching fresh views.
          if (
            /(role|binding|membership|disabled|credential|status_changed|recovery|suspend)/.test(
              event.kind || "",
            )
          ) {
            void query.resetQueries({
              predicate: (q) => q.queryKey[0] === scope,
            });
          }
          reconcile();
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
    };
    window.addEventListener("online", online);
    window.addEventListener("offline", online);
    return () => {
      disposed = true;
      source?.close();
      clearTimeout(timer);
      clearInterval(safety);
      window.removeEventListener("online", online);
      window.removeEventListener("offline", online);
    };
  }, [scope, conversation, query]);
  return status;
}
