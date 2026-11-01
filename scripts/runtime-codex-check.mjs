// Optional native qualification. Explicitly run with BEEPGRID_CODEX_CHECK=1.
// Uses an existing ChatGPT CLI login, never an API key or a new credential.
import { spawn, spawnSync } from "node:child_process";
import { createInterface } from "node:readline";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import assert from "node:assert/strict";
import { codexBinding } from "../sdk/typescript/dist/runtime/bindings.js";
import { liveInput } from "../sdk/typescript/dist/runtime/index.js";
if (process.env.BEEPGRID_CODEX_CHECK !== "1") {
  console.log(
    "SKIP native Codex: set BEEPGRID_CODEX_CHECK=1 to use the existing signed-in CLI.",
  );
  process.exit(0);
}
const binary = process.env.CODEX_BINARY || "codex";
const env = { ...process.env };
delete env.OPENAI_API_KEY;
const login = spawnSync(binary, ["login", "status"], {
  env,
  encoding: "utf8",
  stdio: ["ignore", "pipe", "pipe"],
});
if (
  login.status !== 0 ||
  !/ChatGPT/i.test((login.stdout || "") + (login.stderr || ""))
) {
  throw Error(
    "Native qualification requires an existing ChatGPT login; no API fallback is allowed",
  );
}
const dir = mkdtempSync(join(tmpdir(), "beepgrid-codex-input-"));
const child = spawn(binary, ["app-server"], {
  cwd: dir,
  env,
  stdio: ["pipe", "pipe", "pipe"],
});
child.stderr.resume();
let sequence = 0,
  active,
  thread,
  finished,
  artifact,
  steeringAt,
  acceptedAt,
  firstUpdateAt,
  failed;
const pending = new Map(),
  segments = [];
let submit;
const write = (value) => child.stdin.write(JSON.stringify(value) + "\n");
const rpc = (method, params) =>
  new Promise((resolve, reject) => {
    const id = ++sequence;
    pending.set(id, { resolve, reject });
    write({ id, method, params });
  });
const binding = codexBinding({
  rpc,
  route: async () => thread,
  activeTurn: () => active,
  idle: async () => {
    while (active) await new Promise((r) => setTimeout(r, 10));
  },
  reconcile: async () => "unknown",
});
const timeout = setTimeout(() => {
  failed = Error("Native check exceeded 90 seconds");
  child.kill("SIGTERM");
}, 90000);
const reader = createInterface({ input: child.stdout });
reader.on("line", (line) => {
  try {
    const message = JSON.parse(line),
      p = message.params || {};
    if (message.id !== undefined && !message.method) {
      const wait = pending.get(message.id);
      pending.delete(message.id);
      if (message.error)
        wait?.reject(
          Error("Native RPC rejected " + JSON.stringify(message.error)),
        );
      else wait?.resolve(message.result);
      return;
    }
    if (message.method === "turn/started") active = p.turn.id;
    if (message.method === "turn/completed") {
      finished = p.turn;
      active = undefined;
    }
    if (
      message.method === "item/agentMessage/delta" &&
      steeringAt &&
      !firstUpdateAt
    )
      firstUpdateAt = performance.now();
    if (message.method === "item/tool/call") {
      const args =
        typeof p.arguments === "string" ? JSON.parse(p.arguments) : p.arguments;
      if (p.tool === "review_segment") {
        segments.push(args.segment);
        if (!steeringAt) {
          steeringAt = performance.now();
          submit = binding
            .submit(
              liveInput({
                id: "native-input-check",
                generation: 1,
                payload: {
                  type: "message",
                  conversation_id: "native-check",
                  message: {
                    id: "correction",
                    sender_id: "person_owner",
                    body: "Quick update: briefly tell me which segment you reviewed so far, then continue all six segments. Use DELTA instead of ALPHA for the final report label.",
                  },
                },
              }),
              "steer",
            )
            .then(() => (acceptedAt = performance.now()))
            .catch((error) => {
              failed = error;
            });
        }
        setTimeout(
          () =>
            write({
              id: message.id,
              result: {
                success: true,
                contentItems: [
                  {
                    type: "inputText",
                    text: JSON.stringify({
                      segment: args.segment,
                      checks: 12,
                      risk: "low",
                    }),
                  },
                ],
              },
            }),
          400,
        );
      } else if (p.tool === "complete_review") {
        artifact = args;
        write({
          id: message.id,
          result: {
            success: true,
            contentItems: [{ type: "inputText", text: "Report recorded." }],
          },
        });
      } else
        write({
          id: message.id,
          error: { code: -32601, message: "Unsupported check tool" },
        });
    } else if (message.id !== undefined && message.method)
      write({
        id: message.id,
        error: { code: -32601, message: "Unsupported native control request" },
      });
  } catch (error) {
    failed = error;
    child.kill("SIGTERM");
  }
});
try {
  await rpc("initialize", {
    clientInfo: { name: "beepgrid-binding-check", version: "0.1.0" },
    capabilities: { experimentalApi: true },
  });
  write({ method: "initialized" });
  const models = await rpc("model/list", {});
  assert.ok(
    models.data.some(
      (m) => m.id === "gpt-6.1-sol" || m.model === "gpt-6.1-sol",
    ),
    "Selected model is not in the native catalog",
  );
  const tools = [
    {
      name: "review_segment",
      description: "Review one numbered section.",
      inputSchema: {
        type: "object",
        properties: { segment: { type: "integer", minimum: 1, maximum: 6 } },
        required: ["segment"],
        additionalProperties: false,
      },
    },
    {
      name: "complete_review",
      description: "Record the final review.",
      inputSchema: {
        type: "object",
        properties: {
          label: { type: "string" },
          segments: { type: "array", items: { type: "integer" } },
        },
        required: ["label", "segments"],
        additionalProperties: false,
      },
    },
  ];
  const created = await rpc("thread/start", {
    model: "gpt-6.1-sol",
    cwd: dir,
    sandbox: "read-only",
    approvalPolicy: "never",
    ephemeral: true,
    dynamicTools: tools,
  });
  thread = created.thread.id;
  await rpc("turn/start", {
    threadId: thread,
    effort: "medium",
    input: [
      {
        type: "text",
        text: "Review segments 1 through 6 sequentially using review_segment, one tool call at a time. Do not use other tools or inspect files. Then call complete_review with label ALPHA and all six segment numbers. Briefly summarize the result.",
      },
    ],
  });
  while (!finished && !failed) {
    if (child.exitCode !== null) throw Error("Native server exited");
    await new Promise((r) => setTimeout(r, 20));
  }
  if (failed) throw failed;
  await submit;
  assert.equal(finished.status, "completed");
  assert.equal(artifact.label, "DELTA");
  assert.deepEqual(segments, [1, 2, 3, 4, 5, 6]);
  assert.deepEqual(artifact.segments, [1, 2, 3, 4, 5, 6]);
  const result = {
    native: "Codex app-server",
    model: "gpt-6.1-sol",
    mode: "steer",
    read_only: true,
    api_key_fallback: false,
    accepted_ms: acceptedAt - steeringAt,
    first_update_ms: firstUpdateAt ? firstUpdateAt - steeringAt : null,
    segments,
    final_label: artifact.label,
    completed: true,
  };
  if (process.env.BEEPGRID_CHECK_OUTPUT)
    writeFileSync(
      process.env.BEEPGRID_CHECK_OUTPUT,
      JSON.stringify(result, null, 2),
    );
  console.log(JSON.stringify(result, null, 2));
} finally {
  clearTimeout(timeout);
  child.kill("SIGTERM");
  reader.close();
  for (const wait of pending.values())
    wait.reject(Error("Native check closed"));
  rmSync(dir, { recursive: true, force: true });
}
