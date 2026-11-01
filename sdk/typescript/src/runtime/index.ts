import type { Client, Delivery } from "../index.js";

export type BusyMode = "steer" | "queue";
export type LiveInput = {
  /** Stable across transport lease generations. Use this in host checkpoints. */
  id: string;
  conversationId: string;
  kind: string;
  delivery: Delivery;
};
export type StoredInput = LiveInput & { state: "pending" | "submitting" | "accepted" };
export interface InboxStore {
  put(input: LiveInput): void;
  pending(): StoredInput[];
  setState(id: string, state: StoredInput["state"]): void;
  get(id: string): StoredInput | undefined;
  close(): void;
}
export interface InputBinding {
  readonly name: string;
  readonly supportsSteer: boolean;
  /** Return after native acceptance/checkpoint, never after the whole business job.
   * Honor input.id for deduplication. Do not elevate peer content to owner authority. */
  submit(input: LiveInput, mode: BusyMode, signal?: AbortSignal): Promise<void>;
  resume?(input: LiveInput, signal?: AbortSignal): Promise<void>;
  /** A crash/network error may leave native acceptance uncertain. No blind replay. */
  reconcile?(input: LiveInput): Promise<"accepted" | "retry" | "unknown">;
}
export function liveInput(delivery: Delivery): LiveInput {
  const conversationId = delivery.payload.conversation_id || delivery.payload.request?.conversation_id || delivery.payload.message?.conversation_id;
  if (!delivery.id || !conversationId) throw new Error("Delivery has no stable conversation identity");
  return { id: delivery.id, conversationId, kind: delivery.payload.type, delivery };
}
/** Serialized receiver with independent intake and native submission loops.
 * Own one worker credential exclusively. Do not also call worker.run/MCP receive. */
export class LiveConnection {
  private accepting?: Promise<void>;
  private pumping?: Promise<void>;
  private stopped = false;
  private ready = false;
  private nudge?:()=>void;
  private controller?: AbortController;
  private failure?: unknown;
  private active = new Map<string, Promise<void>>();
  private checked = new Map<string, number>();
  constructor(readonly client: Pick<Client, "inbox" | "ack">, readonly store: InboxStore,
    readonly binding: InputBinding, readonly mode: BusyMode = "steer",
    readonly onError: (error: unknown) => void = () => {}) {
    if (!["steer", "queue"].includes(mode)) throw new Error("Use steer or queue");
    if (mode === "steer" && !binding.supportsSteer) throw new Error(`${binding.name} supports Queue only`);
  }
  /** Non-blocking start. Stop and await done() before closing the store. */
  start(signal?: AbortSignal): this {
    if (this.controller) throw new Error("Connection already started");
    this.controller = new AbortController();
    if (signal?.aborted) this.controller.abort(signal.reason);
    else signal?.addEventListener("abort", () => this.stop(), {once:true, signal:this.controller.signal});
    this.accepting = this.intake(this.controller.signal).catch(e => { this.failure = e; this.stop(); });
    this.pumping = this.dispatch(this.controller.signal).catch(e => { this.failure = e; this.stop(); });
    return this;
  }
  stop() { this.stopped = true; this.controller?.abort(); }
  async done() { await Promise.all([this.accepting, this.pumping]); await Promise.all(this.active.values()); if (this.failure) throw this.failure; }
  private async intake(signal: AbortSignal) {
    while (!this.stopped && !signal.aborted) {
      try {
        const deliveries = await this.client.inbox(signal, 15000);
        this.ready=true;
        for (const delivery of deliveries) {
          // Synchronous durable transaction happens before ACK or a host call.
          this.store.put(liveInput(delivery));
          this.nudge?.();
          await this.client.ack(delivery);
        }
        if (!deliveries.length) await pause(50, signal);
      } catch (error) {
        if (signal.aborted) return;
        // Revoked credentials and malformed deliveries require operator action.
        const status = (error as {status?:number}).status;
        if (status && status < 500 && status !== 429 && status !== 409) throw error;
        this.onError(error);
        await pause(500, signal);
      }
    }
  }
  private async dispatch(signal: AbortSignal) {
    while (!signal.aborted) {
      if(!this.ready){await pause(10,signal);continue;}
      const sessions=new Set<string>();
      const pending=this.store.pending();
      // Control results must not sit behind ordinary queued work.
      pending.sort((a,b)=>Number(b.kind === "request.resolved")-Number(a.kind === "request.resolved"));
      for (const input of pending) {
        const lane=input.conversationId+(input.kind === "request.resolved" && this.binding.resume?"/result":"/input");
        if(signal.aborted)break;
        if(this.active.size>=8 && !(input.kind === "request.resolved" && this.binding.resume))continue;
        if(sessions.has(lane) || this.active.has(lane))continue;
        sessions.add(lane);
        if((this.checked.get(input.id)??0)>Date.now()-1000)continue;
        this.checked.set(input.id,Date.now());
        const job=this.deliver(input,signal).catch(error=>{this.onError(error);}).finally(()=>{this.active.delete(lane);this.nudge?.();});
        this.active.set(lane,job);
      }
      await new Promise<void>(resolve=>{
        const wake=()=>{clearTimeout(timer);signal.removeEventListener("abort",wake);this.nudge=undefined;resolve();};
        const timer=setTimeout(wake,1000);this.nudge=wake;signal.addEventListener("abort",wake,{once:true});
        if(signal.aborted)wake();
      });
    }
  }
  private async deliver(input:StoredInput,signal:AbortSignal) {
    if (input.state === "submitting") {
      const outcome=await this.binding.reconcile?.(input)??"unknown";
      if(outcome === "accepted")this.store.setState(input.id,"accepted");
      if(outcome !== "retry")return;
    }
    this.store.setState(input.id,"submitting");
    try {
      if(input.kind === "request.resolved" && this.binding.resume)await this.binding.resume(input,signal);
      else await this.binding.submit(input,input.kind === "request.resolved"?"steer":this.mode,signal);
      this.store.setState(input.id,"accepted");
      this.checked.delete(input.id);
    } catch(error) { this.onError(error); }
  }

}
function pause(ms:number, signal:AbortSignal) {
  if (signal.aborted) return Promise.resolve();
  return new Promise<void>(resolve => {
    const finish=()=>{clearTimeout(timer);signal.removeEventListener("abort",finish);resolve();};
    const timer=setTimeout(finish,ms);signal.addEventListener("abort",finish,{once:true});
  });
}
/** External input stays data. The original delivery retains sender, request,
 * attachments, mentions and authority; bindings must not turn this into system text. */
export function inputText(input:LiveInput):string {
  return JSON.stringify({beepgrid:{...input.delivery.payload,delivery_id:input.id,conversation_id:input.conversationId}});
}
