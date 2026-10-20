import { Client } from "../../sdk/typescript/dist/index.js";
const { client, session } = await new Client(
  process.env.NOTIF_URL,
  process.env.NOTIF_TOKEN,
).connect("examples/typescript", "TypeScript worker", ["message"]);
console.log("Agent:", session.agent.id);
const controller = new AbortController();
process.on("SIGINT", () => controller.abort());
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
