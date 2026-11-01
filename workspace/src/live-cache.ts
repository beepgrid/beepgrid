import type { QueryClient, InfiniteData } from "@tanstack/react-query";
import type { Conversation, Message, Page } from "./contracts";
export type LiveEvent = {
  sequence?: number;
  kind: string;
  conversation_id?: string;
  data?: unknown;
};
export function nextCursor(current: string, candidate: unknown) {
  const number = Number(candidate);
  return Number.isSafeInteger(number) && number > Number(current || 0)
    ? String(number)
    : current;
}
export function applyLiveEvent(
  query: QueryClient,
  scope: string,
  selected: string,
  event: LiveEvent,
) {
  const kind = event.kind;
  if (
    /(role|binding|group\.saved|membership|access_changed|disabled|credential|status_changed|recovery|suspend|assistance\.)/.test(
      kind,
    )
  ) {
    void query.resetQueries({ predicate: (q) => q.queryKey[0] === scope });
    return;
  }
  if (
    kind === "message.created" &&
    event.conversation_id === selected &&
    selected
  ) {
    const m = event.data as Message | undefined;
    if (m?.id && m.conversation_id === selected && typeof m.body === "string") {
      query.setQueryData<Conversation>(
        [scope, "resource", "/conversations/" + selected],
        (old) =>
          old
            ? {
                ...old,
                messages: [
                  ...(old.messages || []).filter((x) => x.id !== m.id),
                  m,
                ]
                  .sort(
                    (a, b) =>
                      a.created_at.localeCompare(b.created_at) ||
                      a.id.localeCompare(b.id),
                  )
                  .slice(-500),
              }
            : old,
      );
      query.setQueriesData<InfiniteData<Page<Message>>>(
        {
          predicate: (q) =>
            q.queryKey[0] === scope &&
            q.queryKey[1] === "collection" &&
            q.queryKey[2] === "messages" &&
            (q.queryKey[4] as Record<string, string>)?.conversation_id ===
              selected,
        },
        (old) => {
          if (
            !old ||
            old.pages.at(-1)?.next_cursor ||
            old.pages.some((p) => p.items.some((x) => x.id === m.id))
          )
            return old;
          return {
            ...old,
            pages: old.pages.map((p, i) =>
              i === old.pages.length - 1 ? { ...p, items: [...p.items, m] } : p,
            ),
          };
        },
      );
    }
  }
  // Heartbeats do not reread every conversation or request. Invalidate only
  // affected families; the 15-second authoritative read recovers missed events.
  const families = new Set<string>();
  if (/message|conversation/.test(kind)) families.add("conversations");
  if (kind === "agent.updated") {
    families.add("conversations");
    families.add("overseer_plans");
  }
  if (/request|response|answer/.test(kind)) {
    families.add("requests");
    families.add("conversations");
    families.add("answers");
  }
  if (/instance|agent|worker|progress/.test(kind)) {
    families.add("agents");
    families.add("instances");
  }
  if (/delivery/.test(kind)) {
    families.add("deliveries");
    families.add("delivery_attempts");
  }
  if (/overseer|request|response|answer/.test(kind))
    families.add("overseer_plans");
  if (/execution/.test(kind)) families.add("executions");
  void query.invalidateQueries({
    predicate: (q) => {
      if (q.queryKey[0] !== scope) return false;
      if (q.queryKey[1] === "collection")
        return families.has(String(q.queryKey[2]));
      if (q.queryKey[1] === "resource") {
        const path = String(q.queryKey[2]);
        return (
          path === "/activity" ||
          (families.has("requests") && path.startsWith("/requests/")) ||
          (families.has("agents") && /fleet|agents|instances/.test(path)) ||
          ((!event.data || kind === "agent.updated") &&
            families.has("conversations") &&
            path === "/conversations/" + selected)
        );
      }
      return (
        q.queryKey[1] === "read" &&
        ((q.queryKey[2] === "/fleet/status" &&
          (families.has("agents") || families.has("requests"))) ||
          (families.has("requests") &&
            q.queryKey[2] === "/capabilities" &&
            !!(q.queryKey[3] as { request_id?: string })?.request_id))
      );
    },
  });
}
