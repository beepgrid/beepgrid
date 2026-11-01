import { inputText, type InputBinding } from "./index.js";
import type { RPC, SessionRoute } from "./bindings.js";

/** Goose's versioned ACP extension, not a capability of arbitrary ACP agents.
 * Feed existing session/update notifications to observe(). Native user echo or
 * output from the newly fenced run releases input before its long turn finishes.
 * Keep the existing event consumer and error reporting alive until turn end. */
export function gooseBinding(options: {
  rpc: RPC;
  route: SessionRoute;
  idle: (session: string) => Promise<void>;
  reconcile: InputBinding["reconcile"];
  onError: (error: unknown) => void;
}): InputBinding & { observe: (params: unknown) => void } {
  const runs = new Map<string, string>();
  const waiting = new Map<
    string,
    { text: string; echo: string; run?: string; accept: () => void }
  >();
  return {
    name: "Goose ACP",
    supportsSteer: true,
    reconcile: options.reconcile,
    observe(params) {
      const p = params as {
        sessionId?: string;
        update?: {
          sessionUpdate?: string;
          _meta?: { goose?: { activeRunId?: string | null } };
          content?: { type?: string; text?: string };
        };
      } | null;
      if (!p?.sessionId || !p.update) return;
      const { sessionId, update } = p;
      const goose = update._meta?.goose;
      if (
        update.sessionUpdate === "session_info_update" &&
        goose &&
        "activeRunId" in goose
      ) {
        if (typeof goose.activeRunId === "string") {
          runs.set(sessionId, goose.activeRunId);
          const pending = waiting.get(sessionId);
          if (pending) pending.run = goose.activeRunId;
        } else if (goose.activeRunId === null) runs.delete(sessionId);
      }
      const pending = waiting.get(sessionId);
      if (
        pending?.run === runs.get(sessionId) &&
        pending?.run &&
        ["agent_message_chunk", "agent_thought_chunk", "tool_call"].includes(
          update.sessionUpdate || "",
        )
      ) {
        // Reviewed Goose start_reply persists the user input before returning
        // the stream forwarded as these updates. One host writer owns this
        // session. activeRunId alone is emitted earlier and is insufficient.
        pending.accept();
        return;
      }
      if (
        pending &&
        update.sessionUpdate === "user_message_chunk" &&
        update.content?.type === "text"
      ) {
        pending.echo += update.content.text || "";
        if (pending.echo === pending.text) pending.accept();
        // Do not retain arbitrary transcript text if the echo differs.
        else if (!pending.text.startsWith(pending.echo)) pending.echo = "";
      }
    },
    async submit(input, mode, signal) {
      const sessionId = await options.route(input.conversationId);
      if (mode === "queue") await options.idle(sessionId);
      const text = inputText(input);
      const prompt = [{ type: "text", text }];
      const run = runs.get(sessionId);
      if (run) {
        const receipt = (await options.rpc(
          "_goose/unstable/session/steer",
          {
            sessionId,
            expectedRunId: run,
            prompt,
          },
          signal,
        )) as { runId?: string; messageId?: string } | null;
        if (receipt?.runId !== run || !receipt.messageId)
          throw new Error("Goose did not confirm fenced steering acceptance");
        return;
      }
      if (waiting.has(sessionId))
        throw new Error("Another Goose prompt is awaiting acceptance");
      await new Promise<void>((resolve, reject) => {
        let accepted = false,
          stopped = false;
        const accept = () => {
          if (stopped) return;
          accepted = true;
          cleanup();
          resolve();
        };
        const abort = () => {
          stopped = true;
          cleanup();
          reject(new Error("Goose input acceptance is uncertain"));
        };
        const cleanup = () => {
          waiting.delete(sessionId);
          signal?.removeEventListener("abort", abort);
        };
        if (signal?.aborted) return abort();
        waiting.set(sessionId, { text, echo: "", accept });
        signal?.addEventListener("abort", abort, { once: true });
        // ACP v1 returns at turn end. A matching native user echo is earlier
        // acceptance. Reviewed Goose output also proves start_reply persisted
        // input; run-start metadata alone precedes persistence and cannot.
        Promise.resolve()
          .then(() => options.rpc("session/prompt", { sessionId, prompt }))
          .then(
            () => {
              if (!accepted) accept();
            },
            (error) => {
              if (accepted || stopped) options.onError(error);
              else {
                cleanup();
                reject(error);
              }
            },
          );
      });
    },
  };
}
