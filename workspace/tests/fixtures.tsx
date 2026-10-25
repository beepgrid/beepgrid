import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { Session } from "../src/data";
import type { Me } from "../src/contracts";
export const me: Me = {
  company: "Acme",
  environment: "test",
  admin: false,
  person: {
    id: "per_self",
    kind: "person",
    name: "Taylor",
    external_id: "taylor",
    metadata: {},
    disabled: false,
    created_at: "2026-10-25T00:00:00Z",
  },
};
export const agent = {
  id: "agt_scout",
  kind: "agent",
  name: "Research assistant",
  external_id: "research/scout",
  metadata: { project: "Product" },
  disabled: false,
  created_at: "2026-10-25T00:00:00Z",
};
export function harness() {
  const query = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return {
    query,
    Wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={query}>
        <Session.Provider value={me}>{children}</Session.Provider>
      </QueryClientProvider>
    ),
  };
}
export function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
export function requestBody(init?: RequestInit) {
  return init?.body ? JSON.parse(init.body as string) : {};
}
