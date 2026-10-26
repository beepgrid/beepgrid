import {
  createContext,
  useContext,
  useRef,
  useState,
  useEffect,
  useCallback,
} from "react";
import {
  QueryClient,
  useQuery,
  useInfiniteQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { api, APIError, Operation } from "./client";
import type { Capability, Me, Page, Target } from "./contracts";

export const client = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 8_000,
      gcTime: 120_000,
      retry: (count, e) =>
        !(e instanceof APIError && e.status < 500) && count < 1,
      refetchOnWindowFocus: true,
    },
    mutations: { retry: false },
  },
});
export const Session = createContext<Me | null>(null);
export function useSession() {
  const me = useContext(Session);
  if (!me) throw Error("Missing session");
  return me;
}
export function useScope() {
  const me = useSession();
  return `${me.company}/${me.environment}/${me.person.id}`;
}
export function useResource<T>(path: string, enabled = true) {
  const scope = useScope();
  const q = useQuery({
    queryKey: [scope, "resource", path],
    queryFn: ({ signal }) => api<T>(path, undefined, { signal }),
    enabled,
    refetchInterval: 15_000,
  });
  return { ...q, data: q.error ? undefined : q.data };
}
export function usePostResource<T>(
  path: string,
  body: unknown,
  enabled = true,
) {
  const scope = useScope();
  const q = useQuery({
    queryKey: [scope, "read", path, body],
    queryFn: ({ signal }) => api<T>(path, body, { signal }),
    enabled,
    refetchInterval: 15_000,
  });
  return { ...q, data: q.error ? undefined : q.data };
}
export function useCapabilities(
  target?: Target | { kind: string; id: string },
  requestId?: string,
  connectionId?: string,
) {
  return usePostResource<Capability>("/capabilities", {
    target: target || {},
    request_id: requestId || undefined,
    connection_id: connectionId || undefined,
  });
}
export function useCollection<T>(
  collection: string,
  search = "",
  filters: Record<string, string> = {},
  enabled = true,
) {
  const scope = useScope();
  const q = useInfiniteQuery({
    queryKey: [scope, "collection", collection, search, filters],
    queryFn: ({ pageParam, signal }) =>
      api<Page<T>>(
        "/query",
        { collection, search, filters, cursor: pageParam, limit: 50 },
        { signal },
      ),
    initialPageParam: "",
    getNextPageParam: (page) => page.next_cursor || undefined,
    enabled,
  });
  return {
    ...q,
    items: q.error ? [] : q.data?.pages.flatMap((p) => p.items || []) || [],
  };
}
export function useOperation(recoveryName?: string) {
  const query = useQueryClient();
  const scope = useScope();
  const op = useRef(
    new Operation(
      recoveryName
        ? "beepgrid:workspace:draft:" + scope + ":operation:" + recoveryName
        : undefined,
    ),
  );
  const [pending, setPending] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const locked = useRef(false);
  const forget = useCallback(() => op.current.accepted(), []);
  async function run<T>(path: string, body: unknown): Promise<T | undefined> {
    if (locked.current || uncertain) return;
    locked.current = true;
    setPending(true);
    setError(null);
    try {
      const value = await op.current.send<T>(path, body);
      op.current.accepted();
      void query.invalidateQueries();
      return value;
    } catch (e) {
      // Several administrative creates have no durable idempotency contract.
      // Do not invite a blind retry when their first result may have committed.
      const unkeyedCreate = [
        "/people",
        "/services",
        "/groups",
        "/roles",
        "/connections",
        "/destinations",
        "/credentials",
        "/answers",
      ].includes(path);
      if (unkeyedCreate && (!(e instanceof APIError) || e.status >= 500)) {
        setUncertain(true);
        setError(
          new Error(
            "The result is unknown. Close this form and refresh the current records before creating anything again. For an issued token with a lost secret, revoke its metadata record and issue a replacement.",
          ),
        );
        void query.invalidateQueries();
      } else setError(e as Error);
      if (e instanceof APIError && [403, 404, 409].includes(e.status))
        await query.invalidateQueries();
      return undefined;
    } finally {
      locked.current = false;
      setPending(false);
    }
  }
  return {
    run,
    previous: <T,>(path: string) => op.current.previous<T>(path),
    pending: pending || uncertain,
    uncertain,
    error,
    clear: () => setError(null),
    forget,
  };
}
const draftPrefix = "beepgrid:workspace:draft:";
export function clearDrafts() {
  try {
    for (const k of Object.keys(sessionStorage))
      if (k.startsWith(draftPrefix)) sessionStorage.removeItem(k);
  } catch {}
}
export function useDraft(name: string) {
  const scope = useScope();
  const key = draftPrefix + scope + ":" + name;
  const [value, setValue] = useState(() => {
    try {
      return sessionStorage.getItem(key) || "";
    } catch {
      return "";
    }
  });
  useEffect(() => {
    try {
      if (value) sessionStorage.setItem(key, value);
      else sessionStorage.removeItem(key);
    } catch {}
  }, [key, value]);
  return [value, setValue] as const;
}
