"""Complete Python API tour. Run only in a disposable, empty test workspace.

NOTIF_URL and an owner-only NOTIF_TOKEN_FILE are required. Coordinator examples
also require an engine configured with a model provider. The verification runner
uses a deterministic local provider, not a live model. No external tool executes.
"""
# region setup
import os
from pathlib import Path
import time
import uuid
from beepgrid import Client, mention_context

credential = Path(os.environ['NOTIF_TOKEN_FILE'])
if credential.stat().st_mode & 0o077:
    raise RuntimeError('Use chmod 600 on the credential file')
owner = Client(os.environ['NOTIF_URL'], credential.read_text().strip())
run_id = uuid.uuid4().hex
person_id = owner.call('GET', '/api/v1/me')['person']['id']
builder, builder_session = owner.connect('tour/' + run_id + '/builder', 'Builder 7')
billing, billing_session = owner.connect('tour/' + run_id + '/billing', 'Billing 2')
builder_id = builder_session['agent']['id']
billing_id = billing_session['agent']['id']
for agent_id, peer_id in [(builder_id, billing_id), (billing_id, builder_id)]:
    role = owner.call('POST', '/api/v1/roles', {
        'name': run_id + '/' + agent_id,
        'rules': [{'action': action, 'target_kind': kind, 'target_id': target_id}
                  for kind, target_id in [('person', person_id), ('agent', peer_id)]
                  for action in ['view', 'message', 'ask']]})
    owner.call('POST', '/api/v1/role-bindings', {
        'role_id': role['id'], 'subject_kind': 'principal', 'subject_id': agent_id})
target = {'kind': 'agent', 'id': billing_id}
targets = [{'kind': 'agent', 'id': builder_id}, target]
# endregion setup

# region messages
contacts = builder.directory('agent')
assert any(row['id'] == billing_id for row in contacts['items'])
sent = builder.send(target=target, body='Check the payment ledger for orders 241 and 242.', key=run_id + '/message')
billing.send(conversation_id=sent['conversation_id'], body='Both payments settled yesterday.')
assert len(builder.conversation(sent['conversation_id'])['messages']) == 2
receipt_options = dict(target=target, body='The reconciliation is complete.', key=run_id + '/receipt')
receipt = builder.notify(**receipt_options)
full = builder.send(**receipt_options)
assert full['conversation_id'] == receipt['conversation_id']
preview = owner.dispatch_preview(targets, 'message')
batch = owner.send_batch(targets, 'Hold the staging rollout until checkout jobs drain.',
                        preview_token=preview['token'], key=run_id + '/batch')
assert len({item['result']['conversation_id'] for item in batch}) == 2
# endregion messages
if os.getenv("NOTIF_TOUR_MESSAGES_ONLY") == "1":
    print("PASS: Python first exchange")
    raise SystemExit(0)

# region requests
request = builder.ask(target=target, body='Which region contains order 241?', choices=['EU', 'US'], key=run_id + '/ask')
assert request['expires_at'].startswith('9999-')  # Ordinary communication has no implicit deadline.
current = billing.get_request(request['id'])
billing.respond(request['id'], 'answered', version=current['version'], answer='EU')
assert builder.wait(request['id'], timeout=5)['response']['answer'] == 'EU'
pending = builder.ask(target=target, body='Check order 243 as well.')
replacement = builder.replace_request(pending['id'], pending['version'], 'Check orders 243 and 244.', key=run_id + '/replace')
revised = replacement['replacement']
assert builder.cancel(revised['id'], revised['version'])['state'] == 'cancelled'
batched = owner.ask_batch(targets, {'body': 'Is staging available?'}, key=run_id + '/ask-batch')
assert len(batched) == 2
blocking = builder.ask(target=target, body='I need the ledger before continuing.')
assert builder.set_request_blocking(blocking['id'], True)['blocking']
builder.set_request_blocking(blocking['id'], False)
# endregion requests

# region chats
body = '👋 @Billing 2 check these orders.'
chat = owner.start_chat(targets=targets, body=body,
    mentions=[{'principal_id': billing_id, 'start': 2, 'end': 12}], key=run_id + '/chat')
chat_id = chat['conversation_id']
assert 'explicitly addressed' in mention_context(chat['messages'][0], billing_id)
history = owner.conversation(chat_id)
changed = owner.change_chat_membership(chat_id, operation='rename', version=history['version'],
                                       title='Checkout reconciliation', key=run_id + '/rename')
assert changed['version'] > history['version']
# endregion chats

# region files
pdf = b'%PDF-1.4\nPayment reconciliation fixture\n%%EOF'
draft = builder.stage_attachment('orders.pdf', 'application/pdf', pdf)
with_file = builder.send(target=target, body='Please check these payment records.', attachment_ids=[draft['id']])
listed = billing.attachments(with_file['conversation_id'])
assert listed[0]['id'] == draft['id']
assert billing.download_attachment(draft['id']) == pdf
uploaded = builder.upload_attachment(with_file['conversation_id'], 'notes.txt', 'text/plain', b'Orders 241 and 242')
assert uploaded['id'] != draft['id']
# endregion files

# region queries
page = owner.query('messages', filters={'conversation_id': sent['conversation_id']}, limit=1)
seen = list(page['items'])
while page.get('next_cursor'):
    page = owner.query('messages', filters={'conversation_id': sent['conversation_id']}, limit=1, cursor=page['next_cursor'])
    seen.extend(page['items'])
assert len(seen) == 2
assert owner.capabilities(request_id=blocking['id'])['operations']['cancel']
assert 'roles' in owner.access()
assert owner.request_trace(blocking['id'])['request']['id'] == blocking['id']
assert owner.workspace()['deployment'] == 'self_hosted'
assert 'applied' in owner.account()
assert owner.provider_catalog()
connection = owner.call('POST', '/api/v1/connections', {
    'name': 'Tour webhook ' + run_id, 'kind': 'webhook', 'enabled': False,
    'config': {'url': 'https://example.com/beepgrid'}, 'secret': uuid.uuid4().hex})
assert owner.connection_setup(connection['id'])
# endregion queries

# region workers
billing.heartbeat()
for delivery in billing.inbox():
    # A real worker stores or handles the delivery before acknowledging its lease.
    assert delivery['id'] and delivery['generation']
    billing.ack(delivery)
builder.send(target=target, body='A final worker event.')
processed = []
def handle(delivery):
    processed.append(delivery['id'])
billing.run(handle, stop=lambda: bool(processed))
assert processed
# endregion workers

# region events
stream = billing.events()
try:
    for event in stream:
        if event['kind'] == 'message.created' and event.get('conversation_id') == sent['conversation_id']:
            checkpoint = event['sequence']  # Persist after your application applies this event.
            assert checkpoint > 0
            break
finally:
    stream.close()
# endregion events

# region execution
# Optional advanced contract. This example reports a fixture, not a real deletion.
action = {'name': 'reconcile-orders', 'executor_id': builder_id, 'resource': 'orders',
          'resource_version': 'v2', 'arguments': {'ids': [241, 242]}, 'operation_key': run_id + '/business'}
approval = owner.ask(target={'kind': 'person', 'id': person_id}, body='Apply this reconciliation?', kind='approval', action=action)
owner.respond(approval['id'], 'approved', version=approval['version'])
execution = builder.redeem(approval['id'], action)
assert builder.report_execution(execution['id'], 'succeeded', {'fixture': True})['state'] == 'succeeded'
# endregion execution

# region coordinator
settings = owner.assistance()
assert settings['overseer_configured'], 'Configure NOTIF_AI_PROVIDER and NOTIF_AI_MODEL on the engine first'
owner.update_assistance(overseer=True, version=settings['version'])
plan = owner.plan_overseer('Ask the available specialists whether staging is ready.')
assert owner.overseer_plan(plan['id'])['id'] == plan['id']
assert any(item['id'] == plan['id'] for item in owner.overseer_plans())
confirmed = owner.confirm_overseer(plan['id'])
assert confirmed['id'] == plan['id']
time.sleep(1.1)  # Assistance has a per-person rate limit.
summary = owner.summarize_overseer(plan['id'])
assert summary['id'] == plan['id']
time.sleep(1.1)
automatic = owner.coordinate('Ask the specialists to report any release blockers.', key=run_id + '/coordinate')
assert automatic['id']
# endregion coordinator
print('PASS: Python developer tour')
