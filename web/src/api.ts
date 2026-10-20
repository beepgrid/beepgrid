export type Entity = {
  id: string;
  kind: "person" | "agent" | "service";
  name: string;
  external_id: string;
  metadata: Record<string, unknown>;
  disabled: boolean;
  created_at: string;
  instances?: Instance[];
};
export type Target = { kind: string; id: string };
export type Rule = {
  conditions?: {
    resource?: string;
    resource_version?: string;
    currency?: string;
    max_amount_minor?: number;
    before?: string;
  };
  action: string;
  target_kind: string;
  target_id: string;
  connection_id?: string;
  action_name?: string;
};
export type RequestItem = {
  requester_instance_id?: string;
  id: string;
  requester_id: string;
  conversation_id: string;
  kind: "question" | "approval" | "task";
  title: string;
  body: string;
  choices: string[];
  state: string;
  version: number;
  response?: {
    outcome: string;
    answer?: unknown;
    source_invalidated?: boolean;
  };
  resolved_by?: string;
  expires_at: string;
  created_at: string;
  action?: Record<string, unknown>;
  shared_key?: string;
};
export type Conversation = {
  id: string;
  title: string;
  created_by: string;
  created_at: string;
  participants?: Entity[];
  messages?: {
    id: string;
    body: string;
    sender_id: string;
    instance_id?: string;
    created_at: string;
  }[];
};
export type Group = { id: string; name: string; members: string[] };
export type Role = { id: string; name: string; rules: Rule[] };
export type Connection = {
  id: string;
  name: string;
  kind: string;
  config: Record<string, string>;
  enabled: boolean;
  secret_configured: boolean;
};
export type EventItem = {
  sequence: number;
  kind: string;
  actor_id: string;
  data: unknown;
  created_at: string;
};
export type Instance = {
  id: string;
  agent_id: string;
  presence: string;
  last_seen: string;
  capabilities: string[];
  progress: Record<string, unknown>;
};
export type Me = {
  can_publish_answers?: boolean;
  person: Entity;
  admin: boolean;
  company: string;
  environment: string;
  instance_id?: string;
};
export class APIError extends Error {
  constructor(
    public code: string,
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
export async function api<T>(
  path: string,
  body?: unknown,
  options: RequestInit = {},
): Promise<T> {
  const r = await fetch("/api/v1" + path, {
    ...options,
    method: body === undefined ? "GET" : "POST",
    headers: {
      "Content-Type": "application/json",
      ...(body === undefined ? {} : { "Idempotency-Key": crypto.randomUUID() }),
      ...options.headers,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await r.json();
  if (!r.ok)
    throw new APIError(
      data.error?.code ?? "HTTP_ERROR",
      data.error?.message ?? "The request failed.",
      r.status,
    );
  return data;
}
export function timeAgo(value: string): string {
  const seconds = Math.max(0, (Date.now() - new Date(value).getTime()) / 1000);
  return seconds < 60
    ? "just now"
    : seconds < 3600
      ? `${Math.floor(seconds / 60)}m ago`
      : seconds < 86400
        ? `${Math.floor(seconds / 3600)}h ago`
        : new Date(value).toLocaleDateString();
}
export function requestLabel(r: RequestItem) {
  return r.state === "resolved" ? (r.response?.outcome ?? "resolved") : r.state;
}

export function questionGroupKey(r: RequestItem) {
  return r.kind === "question" && r.state === "open" && r.shared_key
    ? JSON.stringify([r.shared_key, r.title, r.body, r.choices ?? []])
    : r.id;
}
