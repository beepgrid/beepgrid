"""Deterministic chat recipes. Use only a disposable test workspace's owner credential.

Creates two fixture agents and minimal contact grants. Both agents communicate
with their own credentials. No model, external provider or business tool runs.
"""
import argparse
import os
from pathlib import Path
import uuid
from beepgrid import Client

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('workflow', choices=['group', 'selective', 'approval'])
args = parser.parse_args()
path = Path(os.environ['BEEPGRID_TOKEN_FILE'])
if path.stat().st_mode & 0o077:
    raise RuntimeError('Credential file must be owner-only (chmod 600)')
owner = Client(os.environ['BEEPGRID_URL'], path.read_text().strip())
person = owner.call('GET', '/api/v1/me')['person']['id']
run = uuid.uuid4().hex[:12]
builder, first = owner.connect('chat-demo/' + run + '/builder', 'Builder 1')
devops, second = owner.connect('chat-demo/' + run + '/devops', 'DevOps 2')
agents = [(builder, first['agent']['id']), (devops, second['agent']['id'])]
targets = [{'kind': 'agent', 'id': agent_id} for _, agent_id in agents]
for client, agent_id in agents:
    contacts = [('person', person)] + [('agent', other) for _, other in agents if other != agent_id]
    role = owner.call('POST', '/api/v1/roles', {
        'name': 'Chat example ' + run + ' ' + agent_id,
        'rules': [{'action': action, 'target_kind': kind, 'target_id': contact}
                  for kind, contact in contacts for action in ['view', 'message', 'ask']]})
    owner.call('POST', '/api/v1/role-bindings', {
        'role_id': role['id'], 'subject_kind': 'principal', 'subject_id': agent_id})

if args.workflow == 'group':
    body = '@Builder 1 migrate checkout jobs to V2. @DevOps 2 keep staging available until they drain.'
    mentions = [{'principal_id': agent_id, 'start': body.index(name), 'end': body.index(name) + len(name)}
                for name, (_, agent_id) in zip(['@Builder 1', '@DevOps 2'], agents)]
    chat = owner.start_chat(targets=targets, body=body, mentions=mentions, key=run + '/chat')
    chat_id = chat['conversation_id']
    builder.send(conversation_id=chat_id, body='@DevOps 2 the V2 job migration is ready.', key=run + '/builder',
                 mentions=[{'principal_id': second['agent']['id'], 'start': 0, 'end': 9}])
    devops.send(conversation_id=chat_id, body='Staging stays on the old endpoint until the remaining jobs drain.', key=run + '/devops')
    history = owner.conversation(chat_id)
    assert len(history['messages']) == 3
    assert {m['sender_id'] for m in history['messages']} == {person, first['agent']['id'], second['agent']['id']}
    print('PASS: three participants, addressed messages and shared canonical history.')
elif args.workflow == 'selective':
    results = owner.send_batch(targets, 'Pause the staging rollout until checkout jobs drain.', key=run + '/alert')
    ids = [item['result']['conversation_id'] for item in results]
    assert len(set(ids)) == 2
    for (client, _), chat_id in zip(agents, ids):
        client.send(conversation_id=chat_id, body='Holding the rollout; checking the remaining jobs.', key=run + '/' + chat_id)
    # The first agent cannot read the other agent's private conversation.
    from beepgrid import BeepgridError
    try:
        builder.conversation(ids[1])
    except BeepgridError as error:
        assert error.status in (403, 404)
    else:
        raise AssertionError('Private history was exposed')
    print('PASS: two independent conversations and replies; private history stays isolated.')
else:
    request = devops.ask(target={'kind': 'person', 'id': person},
                         body='Restart the staging pool after the checkout jobs drain?', kind='approval', key=run + '/approval')
    chat_id = request['conversation_id']
    owner.send(conversation_id=chat_id, body='Hey, how long will staging be busy?', key=run + '/context')
    devops.send(conversation_id=chat_id, body='About four minutes. Existing checkout jobs finish first; demo data stays unchanged.', key=run + '/reply')
    pending = owner.get_request(request['id'])
    assert pending['state'] == 'open'
    owner.respond(request['id'], 'approved', version=pending['version'])
    approved = devops.wait(request['id'], timeout=10)
    assert approved['response']['outcome'] == 'approved'
    assert len(owner.conversation(chat_id)['messages']) == 2
    print('PASS: follow-up conversation leaves the approval open until the person decides.')
