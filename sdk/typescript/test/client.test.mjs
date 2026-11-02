import { test } from "node:test";
import assert from "node:assert/strict";
import { Client, NotifError } from "../dist/index.js";
test("real question in existing chat without a redundant target", {skip: !process.env.NOTIF_SDK_TEST_URL}, async () => {
  const owner = new Client(process.env.NOTIF_SDK_TEST_URL, process.env.NOTIF_SDK_TEST_TOKEN);
  const me = await owner.call("GET", "/api/v1/me");
  const {client: worker, session} = await owner.connect("sdk/ts-followup-" + crypto.randomUUID(), "Follow-up worker");
  const target = {kind:"agent", id:session.agent.id};
  await owner.saveContactPermissions({subject:target, version:0, rules:[{action:"view", target_kind:"person", target_id:me.person.id}]});
  const chat = await owner.send({target, body:"Review the release"});
  const question = await owner.ask({conversation_id:chat.conversation_id, body:"Which version?"});
  assert.equal(question.conversation_id, chat.conversation_id);
  await worker.respond(question.id, {outcome:"answered", version:question.version, answer:"v2"});
  assert.equal((await owner.getRequest(question.id)).response.answer, "v2");
});
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
    calls.push({url, body:opts.body ? JSON.parse(opts.body):undefined, signal:opts.signal, key:opts.headers["Idempotency-Key"]});
    return new Response(JSON.stringify({id:"ovr_1", overseer:false}));
  });
  const abort = new AbortController();
  const settings=await c.assistance(abort.signal);
  assert.equal(settings.overseer,false);
  await c.updateAssistance({overseer:true});
  await c.planOverseer({prompt:"Ask Scout", target:{kind:"agent",id:"agt_1"},previous_plan_id:"ovr_previous"},abort.signal,"turn-2");
  await c.confirmOverseer("ovr_1");
  await c.overseerPlan("a/b",abort.signal);
  await c.overseerPlans(abort.signal);
  await c.summarizeOverseer("a/b",abort.signal);
  assert.deepEqual(calls[1].body,{overseer:true});
  assert.equal(calls[2].body.previous_plan_id,"ovr_previous");
  assert.equal(calls[2].key,"turn-2");
  assert.deepEqual(calls[3].body,{plan_id:"ovr_1"});
  assert.ok(calls[4].url.endsWith("/fleet/plans/a%2Fb"));
  assert.ok(calls[6].url.endsWith("/fleet/plans/a%2Fb/summary"));
  assert.equal(calls[6].signal,abort.signal);
});

test("real workspace queries and preview-fenced batches",{skip:!process.env.NOTIF_SDK_TEST_URL},async()=>{
 const c=new Client(process.env.NOTIF_SDK_TEST_URL,process.env.NOTIF_SDK_TEST_TOKEN);
 const {session}=await c.connect("sdk/query-ts-"+crypto.randomUUID(),"Query worker");
 const target={kind:"agent",id:session.agent.id};
 const preview=await c.dispatchPreview({targets:[target],operation:"message"});
 const result=await c.sendBatch({targets:[target,target],body:"one",preview_token:preview.token},"batch-"+crypto.randomUUID());
 assert.equal(result.length,1);
 const page=await c.query({collection:"messages",filters:{conversation_id:result[0].result.conversation_id},limit:1});assert.equal(page.items.length,1);
 const owner=await c.call("GET","/api/v1/me");
 const before=await c.previewContactPermissions({subject:target});
 await c.saveContactPermissions({subject:target,version:before.policy.version,rules:[{action:"view",target_kind:"person",target_id:owner.person.id}]});
 const requests=await c.askBatch({targets:[target],request:{title:"Progress?"}});assert.equal(requests.length,1);
 const caps=await c.capabilities({request_id:requests[0].request.id});assert.equal(caps.operations.cancel,true);
 assert.equal((await c.requestTrace(requests[0].request.id)).request.id,requests[0].request.id);
 assert.equal((await c.workspace()).deployment,"self_hosted");assert.ok((await c.account()).applied);assert.ok((await c.access()).roles);
});

test("real recipient preferences preserve inheritance and reject stale edits", {skip: !process.env.NOTIF_SDK_TEST_URL}, async () => {
  const c = new Client(process.env.NOTIF_SDK_TEST_URL, process.env.NOTIF_SDK_TEST_TOKEN);
  const {person} = await c.call("GET", "/api/v1/me");
  const before = await c.deliveryPreferences(person.id);
  const saved = await c.saveDeliveryPreferences(person.id, {...before, default: {mode:"preferred", destinations:[], followup_seconds:0}});
  assert.equal(saved.version, before.version + 1);
  assert.deepEqual((await c.deliveryPreferences(person.id)).default.destinations, []);
  await assert.rejects(c.saveDeliveryPreferences(person.id, before), error => error.status === 409);
  const restored = await c.saveDeliveryPreferences(person.id, {...before, version:saved.version});
  assert.deepEqual(restored.default, before.default);
});

test("real token lifecycle keeps names, limits and rotation metadata", {skip: !process.env.NOTIF_SDK_TEST_URL}, async () => {
  const owner = new Client(process.env.NOTIF_SDK_TEST_URL, process.env.NOTIF_SDK_TEST_TOKEN);
  const me = await owner.call("GET", "/api/v1/me");
  const issued = await owner.createCredential({principal_id:me.person.id,kind:"api",name:"TypeScript reports",permissions:["read"]});
  const meta = (await owner.credentials()).find(c=>c.id===issued.id);
  assert.equal(meta.name,"TypeScript reports");assert.deepEqual(meta.permissions,["read"]);
  const edited = await owner.editCredential(meta.id,{version:meta.version,name:"TypeScript renamed"});
  const replacement = await owner.rotateCredential(meta.id,edited.version,300);
  assert.equal(replacement.credential.id,meta.id);
  await new Client(owner.baseURL,replacement.token).call("GET","/api/v1/me");
  await owner.revokeCredential(meta.id);
  await assert.rejects(new Client(owner.baseURL,replacement.token).call("GET","/api/v1/me"),e=>e.status===401);
  const {client,session}=await owner.connect("sdk/renew-ts-"+crypto.randomUUID(),"Renewable",[],{},{renewable:true,connection_name:"TS live"});
  const renewed=await client.renewCredential();assert.equal(renewed.kind,"worker");assert.ok(renewed.renewable_until);
  assert.equal((await client.call("GET","/api/v1/me")).instance_id,session.instance_id);
  const permanent=await owner.createCredential({principal_id:me.person.id,kind:"api",never_expires:true});
  assert.equal(permanent.expires_at,null);
  const always=await owner.connect("sdk/persistent-ts-"+crypto.randomUUID(),"Persistent",[],{},{never_expires:true});
  assert.equal(always.session.expires_at,null);
  await always.client.heartbeat({});
});
test("rotation never blindly retries an uncertain secret response",async()=>{
  let calls=0;const client=new Client("http://localhost","fixture",async()=>{calls++;throw new TypeError("response lost")});
  await assert.rejects(client.rotateCredential("key_fixture",1));assert.equal(calls,1);
});
