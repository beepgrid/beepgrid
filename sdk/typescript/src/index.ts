import { readEvents, type EventOptions } from "./events.js";
export { type NotifEvent, type EventOptions } from "./events.js";
export type Target = {
  kind: "person" | "agent" | "service" | "group" | "instance";
  id: string;
};
export type Person = {
  id: string;
  name: string;
  external_id: string;
  metadata: Record<string, unknown>;
};
export type Agent = {
  id: string;
  name: string;
  external_id: string;
  metadata: Record<string, unknown>;
};
export type Action = {
  name: string;
  executor_id: string;
  resource: string;
  resource_version: string;
  arguments: Record<string, unknown>;
  operation_key: string;
};
export type Response = {
  outcome:
    | "answered"
    | "approved"
    | "denied"
    | "completed"
    | "failed"
    | "rejected";
  answer?: unknown;
  version: number;
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
  action?: Action;
  state: "open" | "resolved" | "cancelled" | "expired" | "superseded";
  version: number;
  response?: Response & {
    responder_instance_id?: string;
    source_answer_id?: string;
    source_invalidated?: boolean;
    reuse_method?: string;
    reuse_reason?: string;
  };
  expires_at: string;
};
/** An authorized intent and worker-reported state, not proof of an external effect. */
export type Execution = {
  id: string;
  request_id: string;
  executor_id: string;
  operation_key: string;
  action: Action;
  state: "authorized" | "running" | "succeeded" | "failed" | "unknown";
  result: unknown;
};
export type AskInput = {
  preview_token?: string;
  target: Target;
  title: string;
  body?: string;
  kind?: "question" | "approval" | "task";
  conversation_id?: string;
  choices?: string[];
  action?: Action;
  expires_at?: string;
  shared_key?: string;
};
export type Message = {
  id: string;
  conversation_id: string;
  sender_id: string;
  instance_id?: string;
  body: string;
};
export type SendInput = {
  preview_token?: string;
  target?: Target;
  conversation_id?: string;
  body: string;
  title?: string;
};
export type Delivery = {
  id: string;
  generation: number;
  payload: {
    type: string;
    conversation_id: string;
    message?: Message;
    request?: RequestItem;
  };
};
export type Session = {
  token: string;
  agent: Agent;
  instance_id: string;
  expires_at: string;
};
export type AssistanceSettings = {
  automatic_answers: boolean;
  overseer: boolean;
  version: number;
  matcher_configured: boolean;
  overseer_configured: boolean;
};
export type OverseerInput = { prompt: string; target?: Target; previous_plan_id?: string };
export type FleetAgent = {
  agent: Agent;
  fresh: boolean;
  instances: {
    id: string;
    agent_id: string;
    presence: string;
    last_seen: string;
    capabilities: string[];
    progress: Record<string, unknown>;
  }[];
};
export type OverseerPlan = {
  previous_plan_id?: string;
  id: string;
  prompt: string;
  context_agent_ids: string[];
  target?: Target;
  operation: "status" | "message" | "question" | "task";
  summary: string;
  body?: string;
  sources: FleetAgent[];
  requires_confirmation: boolean;
  created_at: string;
  expires_at: string;
  dispatches: {
    agent_id: string;
    conversation_id: string;
    request_id?: string;
  }[];
  replies: RequestItem[];
  reply_summary?: string;
  reply_digest?: string;
};
export class NotifError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly status: number,
  ) {
    super(message);
    this.name = "NotifError";
  }
}
export type QueryInput = {collection: string; search?: string; filters?: Record<string,string>; limit?: number; cursor?: string};
export type QueryPage<T=unknown> = {items:T[]; next_cursor?:string; order:string};
export type CapabilityResult = {observed_at:string; operations:Record<string,boolean>; request_version?:number; outcomes:string[]};
export type DispatchPreviewInput = {targets:Target[]; operation:"message"|"ask"; descriptor?:Action};
export type DispatchPreview = {recipients:unknown[]; count:number; hidden_recipients:number; token:string; expires_at:string; sends:false};
export class Client {
  readonly baseURL: string;
  constructor(
    baseURL: string,
    private readonly token: string,
    private readonly transport: typeof fetch = fetch,
  ) {
    const u = new URL(baseURL);
    if (!["http:", "https:"].includes(u.protocol) || u.username || u.password)
      throw new Error("Invalid Beep Grid server URL");
    this.baseURL = baseURL.replace(/\/$/, "");
  }
  async call<T>(
    method: string,
    path: string,
    body?: unknown,
    options: { key?: string; signal?: AbortSignal; minimal?: boolean } = {},
  ): Promise<T> {
    if (!path.startsWith("/api/v1/")) throw new Error("Invalid API path");
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      Authorization: `Bearer ${this.token}`,
    };
    if (options.key) headers["Idempotency-Key"] = options.key;
    if (options.minimal) headers["Prefer"] = "return=minimal";
    const raw = body === undefined ? undefined : JSON.stringify(body);
    for (let attempt = 0; attempt < 3; attempt++) {
      let response: globalThis.Response;
      try {
        response = await this.transport(this.baseURL + path, {
          method,
          headers,
          body: raw,
          signal: options.signal ?? AbortSignal.timeout(20000),
        });
      } catch (error) {
        if (
          attempt === 2 ||
          options.signal?.aborted ||
          (method !== "GET" && !options.key)
        )
          throw error;
        await delay(200 * (attempt + 1), options.signal);
        continue;
      }
      let value: any;
      try {
        value = await response.json();
      } catch (error) {
        if (
          attempt === 2 ||
          options.signal?.aborted ||
          (method !== "GET" && !options.key)
        )
          throw error;
        await delay(200 * (attempt + 1), options.signal);
        continue;
      }
      if (!response.ok)
        throw new NotifError(
          value.error?.code ?? "HTTP_ERROR",
          value.error?.message ?? `HTTP ${response.status}`,
          response.status,
        );
      return value as T;
    }
    throw new Error("Request attempts exhausted");
  }
  query<T=unknown>(input:QueryInput) {return this.call<QueryPage<T>>("POST","/api/v1/query",input);}
  capabilities(input:{target?:{kind:string;id:string};request_id?:string;connection_id?:string;descriptor?:Action}={}) {return this.call<CapabilityResult>("POST","/api/v1/capabilities",input);}
  access(subject="") {return this.call<unknown>("GET","/api/v1/access?subject_id="+encodeURIComponent(subject));}
  dispatchPreview(input:DispatchPreviewInput) {return this.call<DispatchPreview>("POST","/api/v1/dispatch-preview",input);}
  sendBatch(input:{targets:Target[];body:string;title?:string;preview_token?:string},key:string=crypto.randomUUID()){return this.call<Array<{target:Target;result:{conversation_id:string;messages:Message[]}}>>("POST","/api/v1/message-batches",input,{key});}
  askBatch(input:{targets:Target[];request:Omit<AskInput,"target"|"conversation_id"|"preview_token">;preview_token?:string},key:string=crypto.randomUUID()){return this.call<Array<{target:Target;request:RequestItem}>>("POST","/api/v1/request-batches",input,{key});}
  requestTrace(id:string,cursor=""){return this.call<unknown>("GET","/api/v1/requests/"+encodeURIComponent(id)+"/trace?delivery_cursor="+encodeURIComponent(cursor));}
  workspace(){return this.call<unknown>("GET","/api/v1/workspace");}
  account(){return this.call<unknown>("GET","/api/v1/workspace/account");}
  async connect(
    agent: string,
    name: string,
    capabilities: string[] = [],
    metadata: Record<string, unknown> = {},
  ): Promise<{ client: Client; session: Session }> {
    const session = await this.call<Session>("POST", "/api/v1/sessions", {
      agent,
      name,
      capabilities,
      metadata,
    });
    return {
      client: new Client(this.baseURL, session.token, this.transport),
      session,
    };
  }
  send(input: SendInput, key: string = crypto.randomUUID()) {
    return this.call<{ conversation_id: string; messages: Message[] }>(
      "POST",
      "/api/v1/messages",
      input,
      { key },
    );
  }
  // Confirms durable acceptance only, never provider delivery or execution.
  // send(input, sameKey) can recover the complete canonical receipt later.
  notify(input: SendInput, key: string = crypto.randomUUID()) {
    return this.call<{ conversation_id: string; message_count: number }>(
      "POST",
      "/api/v1/messages",
      input,
      { key, minimal: true },
    );
  }
  ask(input: AskInput, key: string = crypto.randomUUID()) {
    return this.call<RequestItem>("POST", "/api/v1/requests", input, { key });
  }
  getRequest(id: string, signal?: AbortSignal) {
    return this.call<RequestItem>(
      "GET",
      "/api/v1/requests/" + encodeURIComponent(id),
      undefined,
      { signal },
    );
  }
  respond(id: string, response: Response) {
    return this.call<RequestItem>(
      "POST",
      `/api/v1/requests/${encodeURIComponent(id)}/responses`,
      response,
    );
  }
  redeem(id: string, action: Action) {
    return this.call<Execution>(
      "POST",
      `/api/v1/requests/${encodeURIComponent(id)}/redeem`,
      action,
      { key: crypto.randomUUID() },
    );
  }
  reportExecution(
    id: string,
    state: "running" | "succeeded" | "failed" | "unknown",
    result: unknown = null,
  ) {
    return this.call<Execution>(
      "POST",
      `/api/v1/executions/${encodeURIComponent(id)}`,
      { state, result },
      { key: crypto.randomUUID() },
    );
  }
  assistance(signal?: AbortSignal) {
    return this.call<AssistanceSettings>(
      "GET",
      "/api/v1/assistance/settings",
      undefined,
      { signal },
    );
  }
  updateAssistance(
    settings: Partial<
      Pick<AssistanceSettings, "automatic_answers" | "overseer" | "version">
    >,
  ) {
    return this.call<AssistanceSettings>(
      "POST",
      "/api/v1/assistance/settings",
      settings,
    );
  }
  planOverseer(input: OverseerInput, signal?: AbortSignal, key?: string) {
    return this.call<OverseerPlan>("POST", "/api/v1/fleet/plan", input, {
      key, signal,
    });
  }
  overseerPlans(signal?: AbortSignal) {
    return this.call<OverseerPlan[]>("GET", "/api/v1/fleet/plans", undefined, {
      signal,
    });
  }
  overseerPlan(id: string, signal?: AbortSignal) {
    return this.call<OverseerPlan>(
      "GET",
      "/api/v1/fleet/plans/" + encodeURIComponent(id),
      undefined,
      { signal },
    );
  }
  confirmOverseer(id: string) {
    return this.call<OverseerPlan>("POST", "/api/v1/fleet/command", {
      plan_id: id,
    });
  }
  summarizeOverseer(id: string, signal?: AbortSignal) {
    return this.call<OverseerPlan>(
      "POST",
      "/api/v1/fleet/plans/" + encodeURIComponent(id) + "/summary",
      {},
      { signal },
    );
  }
  // Full, durable events contain canonical message data. Consumers can append
  // message.created directly instead of downloading a growing history again.
  events(options: EventOptions = {}) {
    return readEvents(this.baseURL, this.token, this.transport, options);
  }
  async wait(id: string, signal?: AbortSignal): Promise<RequestItem> {
    for (;;) {
      const request = await this.getRequest(id, signal);
      if (request.state !== "open") return request;
      await delay(1000, signal);
    }
  }
  cancel(id: string, version: number) {
    return this.call<RequestItem>(
      "POST",
      `/api/v1/requests/${encodeURIComponent(id)}/cancel`,
      { version },
    );
  }
  inbox(signal?: AbortSignal) {
    return this.call<Delivery[]>("GET", "/api/v1/worker/inbox", undefined, {
      signal,
    });
  }
  ack(delivery: Delivery) {
    return this.call(
      "POST",
      `/api/v1/worker/inbox/${encodeURIComponent(delivery.id)}/ack`,
      { generation: delivery.generation },
    );
  }
  heartbeat(progress: Record<string, unknown> = {}) {
    return this.call("POST", "/api/v1/worker/heartbeat", progress);
  }
  async run(
    handler: (delivery: Delivery) => Promise<void>,
    signal?: AbortSignal,
  ) {
    while (!signal?.aborted) {
      let jobs: Delivery[];
      try {
        jobs = await this.call<Delivery[]>(
          "GET",
          "/api/v1/worker/inbox?wait_ms=15000",
          undefined,
          { signal },
        );
      } catch (e) {
        if (signal?.aborted) throw e;
        if (e instanceof NotifError && e.status < 500 && e.status !== 429)
          throw e;
        await delay(2000, signal);
        continue;
      }
      for (const job of jobs) {
        await handler(job);
        await this.ack(job);
      }
      if (!jobs.length) await delay(100, signal);
    }
  }
}
function delay(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason);
      return;
    }
    const abort = () => {
      clearTimeout(timer);
      reject(signal?.reason ?? new Error("Cancelled"));
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, ms);
    signal?.addEventListener("abort", abort, { once: true });
  });
}
