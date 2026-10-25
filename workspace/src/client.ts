export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}
let expire = () => {};
let sessionGeneration = 0;
export function advanceSession() {
  sessionGeneration++;
}
export function onSessionExpired(handler: () => void) {
  expire = handler;
  return () => {
    expire = () => {};
  };
}
export async function upload<T>(path: string, file: File): Promise<T> {
  const generation = sessionGeneration;
  const body = new FormData();
  body.append("file", file);
  const response = await fetch("/api/v1" + path, {
    method: "POST",
    credentials: "same-origin",
    body,
  });
  const value = await response.json().catch(() => null);
  if (!response.ok) {
    if (response.status === 401 && generation === sessionGeneration) expire();
    throw new APIError(
      response.status,
      value?.error?.code || "UPLOAD_FAILED",
      value?.error?.message ||
        "Upload unavailable. Check current access and try again.",
    );
  }
  return value as T;
}
export async function api<T>(
  path: string,
  body?: unknown,
  options: { signal?: AbortSignal; key?: string; method?: string } = {},
): Promise<T> {
  const generation = sessionGeneration;
  const response = await fetch("/api/v1" + path, {
    method: options.method || (body === undefined ? "GET" : "POST"),
    credentials: "same-origin",
    signal: options.signal,
    headers:
      body === undefined
        ? undefined
        : {
            "Content-Type": "application/json",
            ...(options.key ? { "Idempotency-Key": options.key } : {}),
          },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const value = await response.json().catch(() => null);
  if (!response.ok) {
    if (
      response.status === 401 &&
      generation === sessionGeneration &&
      path !== "/auth/login" &&
      path !== "/me" &&
      path !== "/workspace/account"
    )
      expire();
    throw new APIError(
      response.status,
      value?.error?.code || value?.code || "UNAVAILABLE",
      value?.error?.message ||
        value?.message ||
        "The operation is unavailable. Please try again.",
    );
  }
  return value as T;
}
// A retry retains the operation identity only while both the path and exact body match.
// A scoped recovery journal can retain it across navigation. Acceptance and
// confirmed context revocation clear it; it is never a shared credential store.
export class Operation {
  private signature = "";
  private key = "";
  constructor(private journal?: string) {
    if (journal)
      try {
        const saved = JSON.parse(sessionStorage.getItem(journal) || "null");
        if (saved?.signature && saved?.key) {
          this.signature = saved.signature;
          this.key = saved.key;
        }
      } catch {}
  }
  async send<T>(path: string, body: unknown) {
    const signature = JSON.stringify([path, body]);
    if (signature !== this.signature) {
      this.signature = signature;
      this.key = crypto.randomUUID();
      if (this.journal)
        try {
          sessionStorage.setItem(
            this.journal,
            JSON.stringify({ signature: this.signature, key: this.key }),
          );
        } catch {}
    }
    return api<T>(path, body, { key: this.key });
  }
  accepted() {
    this.signature = "";
    this.key = "";
    if (this.journal)
      try {
        sessionStorage.removeItem(this.journal);
      } catch {}
  }
}
export const bytes = (value: string) => new TextEncoder().encode(value).length;
export const readable = (value: unknown) =>
  typeof value === "string" ? value : JSON.stringify(value, null, 2);
export function timestamp(value?: string) {
  return value
    ? new Date(value).toLocaleString(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
      })
    : "Unavailable";
}
export function relative(value?: string) {
  if (!value) return "No report";
  const seconds = Math.max(0, (Date.now() - Date.parse(value)) / 1000);
  return seconds < 60
    ? "Just now"
    : seconds < 3600
      ? `${Math.floor(seconds / 60)}m ago`
      : seconds < 86400
        ? `${Math.floor(seconds / 3600)}h ago`
        : `${Math.floor(seconds / 86400)}d ago`;
}
