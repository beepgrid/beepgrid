#!/usr/bin/env node
import { environments, setupPrompt } from "../dist/runtime/catalog.js";
const args = process.argv.slice(2);
if (args.includes("--help") || !args.length) {
  console.log(
    "Usage: beepgrid-setup <environment> --url <workspace> --instance <instance-id> [--mode steer|queue]\n\n" +
      environments
        .filter((e) => e.group !== "Advanced")
        .map((e) => e.id + "  " + e.label)
        .join("\n"),
  );
  process.exit(0);
}
function value(flag) {
  const i = args.indexOf(flag);
  return i < 0 ? undefined : args[i + 1];
}
try {
  const id = args[0],
    env = environments.find((e) => e.id === id);
  if (!env) throw Error("Unknown environment. Use --help.");
  const mode = value("--mode") || (env.steer ? "steer" : "queue");
  if (!["steer", "queue"].includes(mode)) throw Error("Use steer or queue");
  const url = value("--url"),
    instance = value("--instance");
  if (!url || !instance)
    throw Error("Workspace URL and exact instance are required");
  console.log(setupPrompt(id, url, mode, instance));
} catch (error) {
  console.error(error.message);
  console.error(
    "Run beepgrid-setup --help for environments and required options.",
  );
  console.error(
    "Guide: https://github.com/beepgrid/beepgrid/blob/main/docs/LIVE-INPUT.md#manual-setup",
  );
  process.exitCode = 1;
}
