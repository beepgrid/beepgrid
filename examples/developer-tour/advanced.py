"""Additional primitive checks using the complete disposable Python tour setup.

Run from the repository root with the same protected local test credential.
Uses the local/provider-configured Coordinator setup from the main tour first.
"""
import runpy
import uuid
from beepgrid import NotifError
state = runpy.run_path('examples/developer-tour/main.py')
owner, builder, billing = (state[name] for name in ['owner', 'builder', 'billing'])
builder_id, billing_id, person_id = (state[name] for name in ['builder_id', 'billing_id', 'person_id'])
run_id = uuid.uuid4().hex

# region publication
key = 'staging-region/' + run_id
question = owner.ask(target={'kind': 'person', 'id': person_id}, body='Which region hosts staging?', shared_key=key)
owner.respond(question['id'], 'answered', version=question['version'], answer='eu-west-1')
published = owner.call('POST', '/api/v1/answers', {
    'request_id': question['id'], 'key': key, 'audience': [builder_id]})
reused = builder.ask(target={'kind': 'person', 'id': person_id}, body='Which region hosts staging?', shared_key=key)
assert reused['state'] == 'resolved'
assert reused['response']['source_answer_id'] == published['id']
assert any(item['id'] == published['id'] for item in owner.call('GET', '/api/v1/answers'))
owner.call('POST', '/api/v1/answers/' + published['id'] + '/invalidate', {})
fresh = builder.ask(target={'kind': 'person', 'id': person_id}, body='Which region hosts staging?', shared_key=key)
assert fresh['state'] == 'open'
# endregion publication

# region history
reviewer, session = owner.connect('advanced/' + run_id + '/reviewer', 'Reviewer 3')
reviewer_id = session['agent']['id']
for subject, contacts in [(reviewer_id, [('person', person_id), ('agent', builder_id), ('agent', billing_id)]),
                          (builder_id, [('agent', reviewer_id)]), (billing_id, [('agent', reviewer_id)])]:
    role = owner.call('POST', '/api/v1/roles', {'name': run_id + '/' + subject,
        'rules': [{'action': action, 'target_kind': kind, 'target_id': contact}
                  for kind, contact in contacts for action in ['view', 'message', 'ask']]})
    owner.call('POST', '/api/v1/role-bindings', {'role_id': role['id'], 'subject_kind': 'principal', 'subject_id': subject})
chat = owner.start_chat(targets=[{'kind': 'agent', 'id': builder_id}, {'kind': 'agent', 'id': billing_id}], body='Private earlier history.')
chat_id = chat['conversation_id']
file = owner.upload_attachment(chat_id, 'earlier.txt', 'text/plain', b'Private earlier file')
history = owner.conversation(chat_id)
owner.change_chat_membership(chat_id, operation='add', version=history['version'], members=[reviewer_id])
assert not reviewer.conversation(chat_id).get('messages', [])
try:
    reviewer.download_attachment(file['id'])
except NotifError as error:
    assert error.status in (403, 404)
else:
    raise AssertionError('Earlier file leaked to a future-only member')
owner.send(conversation_id=chat_id, body='This is visible after joining.')
assert len(reviewer.conversation(chat_id)['messages']) == 1
history = owner.conversation(chat_id)
owner.change_chat_membership(chat_id, operation='remove', version=history['version'], members=[reviewer_id])
try:
    reviewer.conversation(chat_id)
except NotifError as error:
    assert error.status in (403, 404)
else:
    raise AssertionError('Removed member retained current history access')
history = owner.conversation(chat_id)
owner.change_chat_membership(chat_id, operation='add', version=history['version'], members=[reviewer_id], share_history=True)
assert len(reviewer.conversation(chat_id)['messages']) == 2
assert reviewer.download_attachment(file['id']) == b'Private earlier file'
# endregion history

# region fleet
edges = []
for source, target_id in [(builder, billing_id), (billing, reviewer_id), (reviewer, builder_id)]:
    request = source.ask(target={'kind': 'agent', 'id': target_id}, body='I need your result before continuing.', blocking=True)
    edges.append((source, request))
status = owner.call('POST', '/api/v1/fleet/status', {})
assert status  # This is communication evidence, not model execution progress.
for source, request in edges:
    source.set_request_blocking(request['id'], False)
    source.cancel(request['id'], request['version'])
# endregion fleet

# region administration
team = owner.call('POST', '/api/v1/groups', {'name': 'Builders ' + run_id,
    'purpose': 'agent_role', 'members': [builder_id]})
assert owner.query('groups', filters={'purpose': 'agent_role', 'member_id': builder_id})['items']
for collection in ['people', 'agents', 'services', 'groups', 'roles', 'connections', 'destinations',
                   'credentials', 'instances', 'requests', 'conversations', 'answers', 'executions',
                   'deliveries', 'overseer_plans']:
    assert 'items' in owner.query(collection, limit=1)
# History and transport-attempt queries require an explicit visible parent.
assert 'items' in owner.query('messages', filters={'conversation_id': chat_id}, limit=1)
assert 'items' in owner.query('attachments', filters={'conversation_id': chat_id}, limit=1)
delivery_id = owner.query('deliveries', limit=1)['items'][0]['id']
assert 'items' in owner.query('delivery_attempts', filters={'delivery_id': delivery_id}, limit=1)
connection = owner.call('POST', '/api/v1/connections', {'name': 'Disabled webhook ' + run_id,
    'kind': 'webhook', 'enabled': False, 'config': {'url': 'https://example.com/beepgrid'}, 'secret': uuid.uuid4().hex})
destination = owner.call('POST', '/api/v1/destinations', {'connection_id': connection['id'],
    'principal_id': builder_id, 'address': 'builder-7', 'mode': 'copy', 'priority': 10})
assert owner.call('POST', '/api/v1/route-preview', {'target': {'kind': 'agent', 'id': builder_id}, 'action': 'message'})
assert owner.call('GET', '/api/v1/connections/' + connection['id'] + '/diagnostics')
owner.call('POST', '/api/v1/destinations/' + destination['id'] + '/remove', {})
# endregion administration
print('PASS: publication, current history/files, dependency cycle and operator examples')
