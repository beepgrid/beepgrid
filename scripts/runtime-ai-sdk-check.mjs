import assert from "node:assert/strict";
import {
  generateText,
  experimental_createSteering,
  stepCountIs,
} from "../tools/runtime-consumers/node_modules/ai/dist/index.js";
import { MockLanguageModelV3 } from "../tools/runtime-consumers/node_modules/ai/dist/test/index.js";
import { aiSDKBinding } from "../sdk/typescript/dist/runtime/bindings.js";
import { liveInput } from "../sdk/typescript/dist/runtime/index.js";
let began, release;
const started = new Promise((r) => (began = r));
const gate = new Promise((r) => (release = r));
let count = 0,
  wakes = 0;
const model = new MockLanguageModelV3({
  doGenerate: async () => {
    count++;
    if (count === 1) {
      began();
      await gate;
    }
    return {
      content: [{ type: "text", text: "ok" }],
      finishReason: { unified: "stop", raw: "stop" },
      usage: {
        inputTokens: { total: 1, noCache: 1, cacheRead: 0, cacheWrite: 0 },
        outputTokens: { total: 1, text: 1, reasoning: 0 },
      },
      warnings: [],
    };
  },
});
const steering = experimental_createSteering();
const run = generateText({
  model,
  prompt: "Review migration",
  experimental_steering: steering,
  stopWhen: stepCountIs(3),
});
await started;
const binding = aiSDKBinding({
  route: async (c) => c,
  steering: () => steering,
  wake: async () => {
    wakes++;
  },
  queue: async () => {
    throw Error("unexpected Queue");
  },
});
const input = liveInput({
  id: "del_one",
  generation: 1,
  payload: {
    type: "message",
    conversation_id: "con_a",
    message: { sender_id: "person_alice", body: "Keep V1" },
  },
});
const submitted = binding.submit(input, "steer");
release();
await submitted;
await run;
assert.equal(count, 2);
assert.match(JSON.stringify(model.doGenerateCalls[1].prompt), /Keep V1/);
assert.equal(wakes, 0);
await binding.submit({ ...input, id: "del_after_finish" }, "steer");
assert.equal(wakes, 1);
console.log(
  "PASS AI SDK 7.0.159 native step steering, dedup ID and final-boundary wake, no model network",
);
