import { test } from "node:test";
import assert from "node:assert/strict";
import { Client, NotifError } from "../dist/index.js";
test("network retry preserves a single idempotency key", async () => {
  const keys = [];
  let tries = 0;
  const client = new Client("http://localhost", "token", async (_url, opts) => {
    keys.push(opts.headers["Idempotency-Key"]);
    if (++tries === 1) throw new TypeError("connection lost");
    return new Response(JSON.stringify({ id: "req_1", state: "open" }), {
      status: 200,
    });
  });
  const q = await client.ask(
    { target: { kind: "person", id: "alice" }, title: "Continue?" },
    "stable-key",
  );
  assert.equal(q.id, "req_1");
  assert.deepEqual(keys, ["stable-key", "stable-key"]);
});
test("structured errors survive the client boundary", async () => {
  const c = new Client(
    "http://localhost",
    "token",
    async () =>
      new Response(
        JSON.stringify({ error: { code: "POLICY_DENIED", message: "Denied" } }),
        { status: 403 },
      ),
  );
  await assert.rejects(
    c.getRequest("secret"),
    (e) =>
      e instanceof NotifError && e.code === "POLICY_DENIED" && e.status === 403,
  );
});
test("cancelling a waiter does not cancel the request", async () => {
  const calls = [];
  const c = new Client("http://localhost", "token", async (url) => {
    calls.push(url);
    return new Response(JSON.stringify({ id: "req_1", state: "open" }));
  });
  const abort = new AbortController();
  const p = c.wait("req_1", abort.signal);
  setTimeout(() => abort.abort(new Error("stop waiting")), 10);
  await assert.rejects(p);
  assert.ok(calls.every((x) => !x.endsWith("/cancel")));
});
test(
  "real HTTP worker enrollment and private message",
  { skip: !process.env.NOTIF_SDK_TEST_URL },
  async () => {
    const c = new Client(
      process.env.NOTIF_SDK_TEST_URL,
      process.env.NOTIF_SDK_TEST_TOKEN,
    );
    const { client, session } = await c.connect(
      "sdk/ts-" + crypto.randomUUID(),
      "TypeScript worker",
      ["message"],
    );
    const sent = await c.send({
      target: { kind: "agent", id: session.agent.id },
      body: "SDK transport test",
    });
    const jobs = await client.inbox();
    assert.equal(jobs.length, 1);
    assert.equal(jobs[0].payload.message.body, "SDK transport test");
    assert.equal(jobs[0].payload.message.conversation_id, sent.conversation_id);
    await client.ack(jobs[0]);
  },
);

test(
  "real HTTP approval and execution lifecycle",
  { skip: !process.env.NOTIF_SDK_TEST_URL },
  async () => {
    const owner = new Client(
      process.env.NOTIF_SDK_TEST_URL,
      process.env.NOTIF_SDK_TEST_TOKEN,
    );
    const me = await owner.call("GET", "/api/v1/me");
    const { client: worker, session } = await owner.connect(
      "sdk/ts-exec-" + crypto.randomUUID(),
      "TS executor",
    );
    const action = {
      name: "fixture",
      executor_id: session.agent.id,
      resource: "fixture",
      resource_version: "1",
      arguments: { value: 1 },
      operation_key: crypto.randomUUID(),
    };
    const q = await owner.ask({
      kind: "approval",
      title: "Fixture?",
      target: { kind: "person", id: me.person.id },
      action,
    });
    await assert.rejects(worker.redeem(q.id, action), (e) => e.status === 409);
    await owner.respond(q.id, { outcome: "approved", version: q.version });
    const execution = await worker.redeem(q.id, action);
    assert.equal((await worker.redeem(q.id, action)).id, execution.id);
    await assert.rejects(
      worker.redeem(q.id, { ...action, resource_version: "2" }),
      (e) => e.status === 403,
    );
    assert.equal(
      (
        await worker.reportExecution(execution.id, "succeeded", {
          reported: true,
        })
      ).state,
      "succeeded",
    );
    await assert.rejects(
      worker.reportExecution(execution.id, "failed"),
      (e) => e.status === 409,
    );
  },
);

test("lost response body retries with the original operation key", async () => {
  const keys = [];
  let attempts = 0;
  const c = new Client("http://localhost", "token", async (_url, options) => {
    keys.push(options.headers["Idempotency-Key"]);
    if (++attempts === 1)
      return {
        json: async () => {
          throw new TypeError("response disconnected");
        },
      };
    return new Response(JSON.stringify({ id: "request-1", state: "open" }));
  });
  assert.equal(
    (
      await c.ask(
        { target: { kind: "person", id: "person-1" }, title: "Continue?" },
        "persisted-key",
      )
    ).id,
    "request-1",
  );
  assert.deepEqual(keys, ["persisted-key", "persisted-key"]);
});

test("optional assistance helpers retain server authority and cancellation", async () => {
  const calls = [];
  const c = new Client("http://localhost", "person-token", async (url, opts) => {
    calls.push({url, body:opts.body ? JSON.parse(opts.body):undefined, signal:opts.signal});
    return new Response(JSON.stringify({id:"ovr_1", automatic_answers:false, overseer:false}));
  });
  const abort = new AbortController();
  const settings=await c.assistance(abort.signal);
  assert.equal(settings.overseer,false);
  await c.updateAssistance({overseer:true});
  await c.planOverseer({prompt:"Ask Scout", target:{kind:"agent",id:"agt_1"}},abort.signal);
  await c.confirmOverseer("ovr_1");
  await c.overseerPlan("a/b",abort.signal);
  await c.overseerPlans(abort.signal);
  await c.summarizeOverseer("a/b",abort.signal);
  assert.deepEqual(calls[1].body,{overseer:true});
  assert.deepEqual(calls[3].body,{plan_id:"ovr_1"});
  assert.ok(calls[4].url.endsWith("/fleet/plans/a%2Fb"));
  assert.ok(calls[6].url.endsWith("/fleet/plans/a%2Fb/summary"));
  assert.equal(calls[6].signal,abort.signal);
});
