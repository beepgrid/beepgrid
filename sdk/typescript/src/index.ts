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
 attachments?:Attachment[];
 mentions?:Mention[];
 blocking?:boolean;
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
 attachment_ids?:string[];
 mentions?:Mention[];
 blocking?:boolean;
 reuse_direct?:boolean;
  preview_token?: string;
  /** Omit when asking the current participants of conversation_id. */
  target?: Target;
  title?: string;
  body?: string;
  kind?: "question" | "approval" | "task";
  conversation_id?: string;
  choices?: string[];
  action?: Action;
  expires_at?: string;
  shared_key?: string;
};
export type Mention = {principal_id:string;start:number;end:number};
export type Message = {
 received_by?:string[];
 attachments?:Attachment[];
 mentions?:Mention[];
  id: string;
  conversation_id: string;
  sender_id: string;
  instance_id?: string;
  body: string;
};
export type StartChatInput = { targets:Target[]; body:string; title?:string; approval?:boolean; choices?:string[]; mentions?:Mention[]; attachment_ids?:string[] };
export type Attachment = { id:string; conversation_id:string; message_id?:string; request_id?:string; file_name:string; media_type:string; size:number; sha256:string; created_at:string };
export type ChatMembershipInput = {operation:"rename"|"add"|"remove"|"leave";version:number;title?:string;members?:string[];share_history?:boolean;next_manager?:string};
export type ChatMembershipResult = {conversation_id:string;version:number;left?:boolean};
export type Conversation = {id:string;title:string;messages?:Message[];participants:unknown[];shared?:boolean;version?:number;manager_id?:string;history_start?:number;past_participants?:unknown[]};
export type SendInput = {
 attachment_ids?:string[];
 mentions?:Mention[];
 reuse_direct?:boolean;
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
  overseer: boolean;
  version: number;
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
  automatic?: boolean;
  instructions?: {agent_id:string;operation:"message"|"question"|"task";body:string}[];
  summary_error?: string;
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
export type PermissionRule = {effect?: "allow" | "deny"; action: "view" | "message" | "ask" | "decide"; target_kind: Target["kind"]; target_id: string};
export type ContactPermissionsInput = {subject: Target; rules: PermissionRule[]; version: number};
export type ContactPreviewInput = {subject: Target; rules?: PermissionRule[]; version?: number; targets?: Target[]};
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
          redirect: "error",
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
  previewContactPermissions(input: ContactPreviewInput) {return this.call<unknown>("POST","/api/v1/contact-permissions/preview",input);}
  saveContactPermissions(input: ContactPermissionsInput) {return this.call<unknown>("POST","/api/v1/contact-permissions",input);}
  access(subject="") {return this.call<unknown>("GET","/api/v1/access?subject_id="+encodeURIComponent(subject));}
  dispatchPreview(input:DispatchPreviewInput) {return this.call<DispatchPreview>("POST","/api/v1/dispatch-preview",input);}
  sendBatch(input:{targets:Target[];body:string;title?:string;preview_token?:string;attachment_ids?:string[]},key:string=crypto.randomUUID()){return this.call<Array<{target:Target;result:{conversation_id:string;messages:Message[]}}>>("POST","/api/v1/message-batches",input,{key});}
  askBatch(input:{targets:Target[];request:Omit<AskInput,"target"|"conversation_id"|"preview_token">;preview_token?:string},key:string=crypto.randomUUID()){return this.call<Array<{target:Target;request:RequestItem}>>("POST","/api/v1/request-batches",input,{key});}
  requestTrace(id:string,cursor=""){return this.call<unknown>("GET","/api/v1/requests/"+encodeURIComponent(id)+"/trace?delivery_cursor="+encodeURIComponent(cursor));}
  workspace(){return this.call<unknown>("GET","/api/v1/workspace");}
  account(){return this.call<unknown>("GET","/api/v1/workspace/account");}
  startChat(input:StartChatInput,key:string=crypto.randomUUID()) { return this.call<{conversation_id:string;messages:Message[]}>("POST","/api/v1/conversations",input,{key}); }
  changeChatMembership(conversationId:string,input:ChatMembershipInput,key:string=crypto.randomUUID()){return this.call<ChatMembershipResult>("POST",`/api/v1/conversations/${encodeURIComponent(conversationId)}/membership`,input,{key});}
  attachments(conversationId:string) { return this.call<Attachment[]>("GET",`/api/v1/conversations/${encodeURIComponent(conversationId)}/attachments`); }
  async uploadAttachment(conversationId:string, file:Blob, name:string, signal?:AbortSignal):Promise<Attachment> {
    return this.uploadFile(`/api/v1/conversations/${encodeURIComponent(conversationId)}/attachments`,file,name,signal);
  }
  /** Private draft for attachment_ids. No automatic retry; unused drafts expire after 24 hours. */
  async stageAttachment(file:Blob,name:string,signal?:AbortSignal):Promise<Attachment> {
    return this.uploadFile("/api/v1/attachments",file,name,signal);
  }
  private async uploadFile(path:string,file:Blob,name:string,signal?:AbortSignal):Promise<Attachment> {
    if (!file.size || file.size>5*1024*1024) throw new Error("Attachments must be between 1 byte and 5 MiB");
    const form=new FormData(); form.append("file",file,name);
    const response=await this.transport(`${this.baseURL}${path}`,{method:"POST",headers:{Authorization:`Bearer ${this.token}`},body:form,signal:signal??AbortSignal.timeout(20000),redirect:"error"});
    const value=await response.json(); if(!response.ok) throw new NotifError(value.error?.code??"HTTP_ERROR",value.error?.message??"Attachment upload rejected",response.status);
    return value as Attachment;
  }
  async downloadAttachment(id:string,signal?:AbortSignal):Promise<Blob> {
    const response=await this.transport(`${this.baseURL}/api/v1/attachments/${encodeURIComponent(id)}`,{headers:{Authorization:`Bearer ${this.token}`},signal:signal??AbortSignal.timeout(20000),redirect:"error"});
    if(!response.ok) { let value:any;try{value=await response.json();}catch{} throw new NotifError(value?.error?.code??"HTTP_ERROR",value?.error?.message??"Attachment download rejected",response.status); }
    if (Number(response.headers.get("Content-Length"))>5*1024*1024) { await response.body?.cancel();throw new Error("Attachment exceeds 5 MiB"); }
    const reader=response.body?.getReader();const chunks:Uint8Array<ArrayBuffer>[]=[];let size=0;
    if(reader) try {for(;;){const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>5*1024*1024)throw new Error("Attachment exceeds 5 MiB");chunks.push(new Uint8Array(value));}}finally{await reader.cancel();reader.releaseLock();}
    return new Blob(chunks,{type:response.headers.get("Content-Type")??"application/octet-stream"});
  }
  directory(kind:"agent"|"person"|"service",search="") { return this.query<Agent|Person>({collection:kind==="person"?"people":kind+"s",search}); }
  conversation(id:string) { return this.call<Conversation>("GET",`/api/v1/conversations/${encodeURIComponent(id)}`); }
  providerCatalog() { return this.call<unknown[]>("GET","/api/v1/integrations/catalog"); }
  connectionSetup(id:string) { return this.call<unknown>("GET",`/api/v1/connections/${encodeURIComponent(id)}/setup`); }
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
      Pick<AssistanceSettings, "overseer" | "version">
    >,
  ) {
    return this.call<AssistanceSettings>(
      "POST",
      "/api/v1/assistance/settings",
      settings,
    );
  }
  setRequestBlocking(id:string, blocking:boolean) {
    return this.call<RequestItem>("POST", `/api/v1/requests/${encodeURIComponent(id)}/blocking`, {blocking});
  }
  coordinate(input:OverseerInput, signal?:AbortSignal, key?:string) {
    return this.call<OverseerPlan>("POST", "/api/v1/fleet/coordinate", input, {key,signal});
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
  replaceRequest(id: string, input: { version: number; body: string; mentions?: Mention[] }, key: string = crypto.randomUUID()) {
    return this.call<{ previous: RequestItem; replacement: RequestItem }>("POST", `/api/v1/requests/${encodeURIComponent(id)}/replace`, input, {key});
  }
  cancel(id: string, version: number) {
    return this.call<RequestItem>(
      "POST",
      `/api/v1/requests/${encodeURIComponent(id)}/cancel`,
      { version },
    );
  }
  inbox(signal?: AbortSignal, waitMs = 0) {
    return this.call<Delivery[]>("GET", `/api/v1/worker/inbox?wait_ms=${Math.max(0, Math.min(15000, Math.floor(waitMs)))}`, undefined, {
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

/** A mention identifies an explicit addressee. Observers may still contribute. */
export function mentionContext(message:Message, recipient:string):string {
 if(!message.mentions?.length)return "";
 return message.mentions.some(m=>m.principal_id===recipient)?"You are explicitly addressed. Other participants may contribute relevant context.":"Other participants are explicitly addressed. You may contribute relevant context.";
}
