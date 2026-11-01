import {
  inputText,
  type BusyMode,
  type InputBinding,
  type LiveInput,
} from "./index.js";

export type SessionRoute = (conversationId: string) => Promise<string>;
/** Use an authenticated, initialized native client, not a URL provided by a message. */
export type RPC = (
  method: string,
  params: Record<string, unknown>,
  signal?: AbortSignal,
) => Promise<unknown>;
type Reconcile = InputBinding["reconcile"];

/** Connect correlated Beepgrid results to the host's existing request waiter.
 * Keep this separate from ordinary turn input so Queue cannot block a result. */
export function withResultResume(
  binding: InputBinding,
  resume: NonNullable<InputBinding["resume"]>,
): InputBinding {
  return { ...binding, resume };
}

/** Codex app-server's documented active-turn API. No cancel/replay fallback.
 * activeTurn must come from the same session's turn events. A turn race remains
 * uncertain until the host transcript is reconciled with the delivery ID. */
export function codexBinding(options: {
  rpc: RPC;
  route: SessionRoute;
  activeTurn: (session: string) => string | undefined;
  idle: (session: string) => Promise<void>;
  reconcile: Reconcile;
}): InputBinding {
  return {
    name: "Codex",
    supportsSteer: true,
    reconcile: options.reconcile,
    async submit(input, mode, signal) {
      const threadId = await options.route(input.conversationId);
      if (mode === "queue") await options.idle(threadId);
      const turn = options.activeTurn(threadId);
      const content = [{ type: "text", text: inputText(input) }];
      if (turn)
        await options.rpc(
          "turn/steer",
          {
            threadId,
            expectedTurnId: turn,
            clientUserMessageId: input.id,
            input: content,
          },
          signal,
        );
      else
        await options.rpc(
          "turn/start",
          { threadId, clientUserMessageId: input.id, input: content },
          signal,
        );
    },
  };
}
/** Pi RPC acknowledges prompt acceptance separately from streamed Agent events. */
export function piBinding(options: {
  rpc: (
    session: string,
    command: Record<string, unknown>,
    signal?: AbortSignal,
  ) => Promise<{ success: boolean; error?: string }>;
  route: SessionRoute;
  reconcile: Reconcile;
}): InputBinding {
  return {
    name: "Pi",
    supportsSteer: true,
    reconcile: options.reconcile,
    async submit(input, mode, signal) {
      const result = await options.rpc(
        await options.route(input.conversationId),
        {
          id: input.id,
          type: "prompt",
          message: inputText(input),
          streamingBehavior: mode === "steer" ? "steer" : "followUp",
        },
        signal,
      );
      if (!result.success) throw new Error(result.error || "Pi rejected input");
    },
  };
}
/** ACP has no universal steering method. Only enable Steer after negotiating a
 * host extension. Queue uses ordinary session/prompt when the session is idle. */
export function acpBinding(options: {
  rpc: RPC;
  route: SessionRoute;
  idle: (session: string) => Promise<void>;
  steer?: (session: string, text: string, id: string) => Promise<void>;
  reconcile: Reconcile;
}): InputBinding {
  return {
    name: "ACP",
    supportsSteer: !!options.steer,
    reconcile: options.reconcile,
    async submit(input, mode, signal) {
      const sessionId = await options.route(input.conversationId);
      if (mode === "steer") {
        if (!options.steer)
          throw new Error("ACP host did not negotiate steering");
        await options.steer(sessionId, inputText(input), input.id);
      } else {
        await options.idle(sessionId);
        await options.rpc(
          "session/prompt",
          { sessionId, prompt: [{ type: "text", text: inputText(input) }] },
          signal,
        );
      }
    },
  };
}
/** AI SDK 7 steering objects are per invocation. The host owns persistent
 * messages and wake(), including final-boundary not-delivered outcomes. */
export function aiSDKBinding(options: {
  route: SessionRoute;
  steering: (session: string) =>
    | undefined
    | {
        steer: (
          message: { role: "user"; content: string },
          options: { id: string },
        ) => Promise<{ type: string; reason?: string }>;
      };
  wake: (session: string, input: LiveInput) => Promise<void>;
  queue: (session: string, input: LiveInput) => Promise<void>;
  reconcile: Reconcile;
}): InputBinding {
  return {
    name: "AI SDK",
    supportsSteer: true,
    reconcile: options.reconcile,
    async submit(input, mode) {
      const session = await options.route(input.conversationId);
      if (mode === "queue") return options.queue(session, input);
      const steering = options.steering(session);
      if (!steering) return options.wake(session, input);
      const result = await steering.steer(
        { role: "user", content: inputText(input) },
        { id: input.id },
      );
      if (result.type === "delivered") return;
      // Host wake() serializes against the finishing run and retains this ID.
      if (result.type === "not-delivered") return options.wake(session, input);
      throw new Error("Unknown AI SDK steering outcome");
    },
  };
}
/** Framework-independent checkpoint port. Suitable for OpenAI Agents, LangGraph,
 * Claude Agent SDK or a custom loop. append() takes durable custody and wake()
 * schedules the existing loop. It must not start a second model-state writer.
 * Correlated results go to resolve(), even in Queue mode. */
export function checkpointBinding(options: {
  name?: string;
  append: (session: string, input: LiveInput, mode: BusyMode) => Promise<void>;
  wake: (session: string) => Promise<void>;
  resolve: (session: string, input: LiveInput) => Promise<void>;
  route: SessionRoute;
  reconcile: Reconcile;
}): InputBinding {
  return {
    name: options.name || "Custom harness",
    supportsSteer: true,
    reconcile: options.reconcile,
    async submit(input, mode) {
      const session = await options.route(input.conversationId);
      await options.append(session, input, mode);
      await options.wake(session);
    },
    async resume(input) {
      await options.resolve(await options.route(input.conversationId), input);
    },
  };
}

/** OpenCode's asynchronous prompt endpoint. Queue only until active injection
 * has been qualified against the installed server version. request() is the
 * user's existing authenticated SDK/HTTP transport. Never cancel a busy run. */
export function openCodeBinding(options: {
  request: (path: string, body: unknown, signal?: AbortSignal) => Promise<void>;
  route: SessionRoute;
  idle: (session: string) => Promise<void>;
  reconcile: Reconcile;
}): InputBinding {
  return {
    name: "OpenCode",
    supportsSteer: false,
    reconcile: options.reconcile,
    async submit(input, _mode, signal) {
      const session = await options.route(input.conversationId);
      await options.idle(session);
      await options.request(
        `/session/${encodeURIComponent(session)}/prompt_async`,
        { parts: [{ type: "text", text: inputText(input) }] },
        signal,
      );
    },
  };
}
/** Claude Agent SDK's streaming input iterator. Yield user messages into the
 * existing query. Queue input is gated by the host's turn-complete event.
 * This is turn input, not interruption of an executing tool. */
export function claudeSDKBinding(options: {
  route: SessionRoute;
  append: (
    session: string,
    message: {
      type: "user";
      message: { role: "user"; content: string };
      parent_tool_use_id: null;
      session_id: string;
    },
    id: string,
    mode: BusyMode,
  ) => Promise<void>;
  reconcile: Reconcile;
}): InputBinding {
  return {
    name: "Claude Agent SDK",
    supportsSteer: false,
    reconcile: options.reconcile,
    async submit(input, mode) {
      const session = await options.route(input.conversationId);
      await options.append(
        session,
        {
          type: "user",
          message: { role: "user", content: inputText(input) },
          parent_tool_use_id: null,
          session_id: session,
        },
        input.id,
        mode,
      );
    },
  };
}
