import { readFileSync, statSync } from "node:fs";
let token=process.env.NOTIF_TOKEN;
if(process.env.NOTIF_TOKEN_FILE){
  if(statSync(process.env.NOTIF_TOKEN_FILE).mode & 0o077)throw new Error("Credential file must be owner-only (chmod 600)");
  token=readFileSync(process.env.NOTIF_TOKEN_FILE,"utf8").trim();
}
if(!token)throw new Error("Set NOTIF_TOKEN_FILE or NOTIF_TOKEN");
import { Client } from "../../sdk/typescript/dist/index.js";
let client = new Client(
  process.env.NOTIF_URL ?? "http://localhost:4010",
  token,
);
const agentId = process.env.NOTIF_AGENT_ID?.trim();
let identity;
if (agentId) {
  const connected = await client.connect(agentId, process.env.NOTIF_AGENT_NAME ?? "TypeScript worker", ["message"]);
  client = connected.client;
  identity = {identity_type: "agent", agent: connected.session.agent, instance_id: connected.session.instance_id};
} else {
  identity = await client.call("GET", "/api/v1/me");
}
if (identity.identity_type !== "agent" || !identity.agent?.id || !identity.instance_id) {
  throw new Error("Without NOTIF_AGENT_ID, supply an existing worker credential for an agent connection");
}
console.log("Agent:", identity.agent.id, "Instance:", identity.instance_id);
const controller = new AbortController();
process.on("SIGINT", () => controller.abort());
process.on("SIGTERM", () => controller.abort());
await client.run(async (delivery) => {
  if (delivery.payload.type === "message") {
    const m = delivery.payload.message;
    await client.send(
      {
        conversation_id: m.conversation_id,
        body: "TypeScript worker received: " + m.body,
      },
      "reply/" + delivery.id,
    );
  }
}, controller.signal);
