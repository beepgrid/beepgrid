import { test } from "node:test";
import assert from "node:assert/strict";
import { environments, setupPrompt } from "../dist/runtime/catalog.js";

test("each live setup prompt routes an AI to a complete credential-free environment guide", () => {
  for (const env of environments.filter((e) => e.group !== "Advanced")) {
    const mode = env.steer ? "steer" : "queue";
    const text = setupPrompt(
      env.id,
      "https://engine.example",
      mode,
      "ins_existing",
    );
    assert.ok(text.includes("docs/LIVE-INPUT.md#" + env.guide), env.id);
    assert.ok(text.includes("docs/LIVE-INPUT.md in the Beepgrid checkout"));
    assert.ok(text.includes("including Manual setup"));
    assert.ok(text.includes("https://github.com/beepgrid/beepgrid#readme"));
    assert.ok(
      text.includes(
        "https://raw.githubusercontent.com/beepgrid/beepgrid/main/docs/LIVE-INPUT.md",
      ),
    );
    assert.ok(text.includes("ins_existing"));
    assert.ok(text.includes(env.binding), env.id);
    assert.ok(text.includes("Ask me for the absolute path"));
    assert.ok(text.includes("NOTIF_TOKEN must be unset"));
    assert.ok(text.includes("not yet a published npm/PyPI install"));
    assert.ok(text.includes("not enroll again"));
    assert.ok(text.includes("restart recovery"));
  }
  for (const origin of [
    "file:///tmp",
    "https://user:secret@engine.example",
    "https://engine.example?token=secret",
    "https://engine.example/path",
  ])
    assert.throws(
      () => setupPrompt("codex", origin, "steer", "ins_existing"),
      /origin/,
    );
  assert.throws(
    () => setupPrompt("codex", "https://engine.example", "steer", "bad\nvalue"),
    /instance/,
  );
  assert.throws(
    () =>
      setupPrompt(
        "gemini-cli",
        "https://engine.example",
        "steer",
        "ins_existing",
      ),
    /Queue only/,
  );
  assert.throws(
    () => setupPrompt("mcp", "https://engine.example", "queue", "ins_existing"),
    /worker or MCP/,
  );
});
