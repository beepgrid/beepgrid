/** Complete JavaScript/TypeScript client tour for a disposable workspace. */
// region setup
import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import { Client, type Target, type Message } from '../../sdk/typescript/dist/index.js';

function readObject(value: unknown): Record<string, unknown> {
  assert.ok(value && typeof value === 'object');
  return value as Record<string, unknown>;
}

const credential = process.env.NOTIF_TOKEN_FILE;
assert.ok(credential && process.env.NOTIF_URL, 'Set NOTIF_TOKEN_FILE and NOTIF_URL');
assert.equal(statSync(credential).mode & 0o077, 0, 'Use chmod 600');
const owner = new Client(process.env.NOTIF_URL, readFileSync(credential, 'utf8').trim());
const runId = crypto.randomUUID();
const personId = (await owner.call<{person:{id:string}}>('GET', '/api/v1/me')).person.id;
const { client: builder, session: builderSession } = await owner.connect(`tour/${runId}/builder`, 'Builder 7');
const { client: billing, session: billingSession } = await owner.connect(`tour/${runId}/billing`, 'Billing 2');
const builderId = builderSession.agent.id, billingId = billingSession.agent.id;
for (const [agentId, peerId] of [[builderId, billingId], [billingId, builderId]]) {
  const rules = [['person', personId], ['agent', peerId]].flatMap(([kind, id]) =>
    ['view', 'message', 'ask'].map(action => ({ action, target_kind: kind, target_id: id })));
  const role = await owner.call<{id:string}>('POST', '/api/v1/roles', { name: `${runId}/${agentId}`, rules });
  await owner.call('POST', '/api/v1/role-bindings', { role_id: role.id, subject_kind: 'principal', subject_id: agentId });
}
const target: Target = { kind: 'agent', id: billingId };
const targets: Target[] = [{ kind: 'agent', id: builderId }, target];
// endregion setup

// region messages
assert.ok((await builder.directory('agent')).items.some(row => row.id === billingId));
const sent = await builder.send({ target, body: 'Check the payment ledger for orders 241 and 242.' }, runId + '/message');
await billing.send({ conversation_id: sent.conversation_id, body: 'Both payments settled yesterday.' });
assert.equal((await builder.conversation(sent.conversation_id)).messages?.length, 2);
const receiptInput = { target, body: 'The reconciliation is complete.' };
const receipt = await builder.notify(receiptInput, runId + '/receipt');
assert.equal((await builder.send(receiptInput, runId + '/receipt')).conversation_id, receipt.conversation_id);
const preview = await owner.dispatchPreview({ targets, operation: 'message' });
const batch = await owner.sendBatch({ targets, body: 'Hold the staging rollout until checkout jobs drain.', preview_token: preview.token }, runId + '/batch');
assert.equal(new Set(batch.map(item => item.result.conversation_id)).size, 2);
// endregion messages
if (process.env.NOTIF_TOUR_MESSAGES_ONLY === "1") { console.log("PASS: TypeScript first exchange"); process.exit(0); }

// region requests
const request = await builder.ask({ target, body: 'Which region contains order 241?', choices: ['EU', 'US'] }, runId + '/ask');
const current = await billing.getRequest(request.id);
await billing.respond(request.id, { outcome: 'answered', version: current.version, answer: 'EU' });
assert.equal((await builder.wait(request.id, AbortSignal.timeout(5000))).response?.answer, 'EU');
const pending = await builder.ask({ target, body: 'Check order 243 as well.' });
const { replacement: revised } = await builder.replaceRequest(pending.id, { version: pending.version, body: 'Check orders 243 and 244.' }, runId + '/replace');
assert.equal((await builder.cancel(revised.id, revised.version)).state, 'cancelled');
assert.equal((await owner.askBatch({ targets, request: { body: 'Is staging available?' } }, runId + '/ask-batch')).length, 2);
const blocking = await builder.ask({ target, body: 'I need the ledger before continuing.' });
assert.equal((await builder.setRequestBlocking(blocking.id, true)).blocking, true);
await builder.setRequestBlocking(blocking.id, false);
// endregion requests

// region chats
const chat = await owner.startChat({ targets, body: '👋 @Billing 2 check these orders.',
  mentions: [{ principal_id: billingId, start: 2, end: 12 }] }, runId + '/chat');
const history = await owner.conversation(chat.conversation_id);
assert.ok(typeof history.version === 'number');
const changed = await owner.changeChatMembership(chat.conversation_id,
  { operation: 'rename', version: history.version, title: 'Checkout reconciliation' }, runId + '/rename');
assert.ok(changed.version > history.version);
// endregion chats

// region files
const pdf = new Blob(['%PDF-1.4\nPayment reconciliation fixture\n%%EOF'], { type: 'application/pdf' });
const draft = await builder.stageAttachment(pdf, 'orders.pdf');
const withFile = await builder.send({ target, body: 'Please check these payment records.', attachment_ids: [draft.id] });
assert.equal((await billing.attachments(withFile.conversation_id))[0].id, draft.id);
assert.equal(await (await billing.downloadAttachment(draft.id)).text(), await pdf.text());
const uploaded = await builder.uploadAttachment(withFile.conversation_id, new Blob(['Orders 241 and 242'], { type: 'text/plain' }), 'notes.txt');
assert.notEqual(uploaded.id, draft.id);
// endregion files

// region queries
const input = { collection: 'messages', filters: { conversation_id: sent.conversation_id }, limit: 1 };
let page = await owner.query<Message>(input), seen = [...page.items];
while (page.next_cursor) {
  page = await owner.query<Message>({ ...input, cursor: page.next_cursor });
  seen.push(...page.items);
}
assert.equal(seen.length, 2);
assert.ok((await owner.capabilities({ request_id: blocking.id })).operations.cancel);
assert.ok('roles' in readObject(await owner.access()));
assert.equal(readObject(readObject(await owner.requestTrace(blocking.id)).request).id, blocking.id);
assert.equal(readObject(await owner.workspace()).deployment, 'self_hosted');
assert.ok(readObject(await owner.account()).applied);
assert.ok((await owner.providerCatalog()).length);
const connection = await owner.call<{id:string}>('POST', '/api/v1/connections', {
  name: 'Tour webhook ' + runId, kind: 'webhook', enabled: false,
  config: { url: 'https://example.com/beepgrid' }, secret: crypto.randomUUID() });
assert.ok(await owner.connectionSetup(connection.id));
// endregion queries

// region workers
await billing.heartbeat();
for (const delivery of await billing.inbox()) {
  assert.ok(delivery.id && delivery.generation);
  await billing.ack(delivery); // Only after applying or durably handing off the delivery.
}
await builder.send({ target, body: 'A final worker event.' });
const workerControl = new AbortController();
let processed = 0;
const timer = setTimeout(() => workerControl.abort(), 500);
try {
  await billing.run(async delivery => { assert.ok(delivery.id); processed++; }, workerControl.signal);
} catch (error) {
  if (!workerControl.signal.aborted) throw error;
} finally { clearTimeout(timer); }
assert.ok(processed > 0);
// endregion workers

// region events
for await (const event of billing.events({ signal: AbortSignal.timeout(5000) })) {
  if (event.kind === 'message.created' && event.conversation_id === sent.conversation_id) {
    const checkpoint = event.sequence; // Persist after applying the event in your application.
    assert.ok(checkpoint > 0);
    break;
  }
}
// endregion events

// region execution
const action = { name: 'reconcile-orders', executor_id: builderId, resource: 'orders', resource_version: 'v2',
  arguments: { ids: [241, 242] }, operation_key: runId + '/business' };
const approval = await owner.ask({ target: { kind: 'person', id: personId }, body: 'Apply this reconciliation?', kind: 'approval', action });
await owner.respond(approval.id, { outcome: 'approved', version: approval.version });
const execution = await builder.redeem(approval.id, action);
assert.equal((await builder.reportExecution(execution.id, 'succeeded', { fixture: true })).state, 'succeeded');
// endregion execution

// region coordinator
const settings = await owner.assistance();
assert.ok(settings.overseer_configured, 'Configure a provider on the engine first');
await owner.updateAssistance({ overseer: true, version: settings.version });
const plan = await owner.planOverseer({ prompt: 'Ask the available specialists whether staging is ready.' });
assert.equal((await owner.overseerPlan(plan.id)).id, plan.id);
assert.ok((await owner.overseerPlans()).some(item => item.id === plan.id));
assert.equal((await owner.confirmOverseer(plan.id)).id, plan.id);
await new Promise(resolve => setTimeout(resolve, 1100));
assert.equal((await owner.summarizeOverseer(plan.id)).id, plan.id);
await new Promise(resolve => setTimeout(resolve, 1100));
assert.ok((await owner.coordinate({ prompt: 'Ask the specialists to report any release blockers.' }, undefined, runId + '/coordinate')).id);
// endregion coordinator
console.log('PASS: TypeScript client developer tour');
