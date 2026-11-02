"""A real Go/Python shared conversation with deterministic fixture messages.

Use only a disposable local workspace's owner credential. Creates three fixture
agents with explicit contact grants. Does not run models or business tools.
"""
import ipaddress
import json
import os
from pathlib import Path
import subprocess
import tempfile
from urllib.parse import urlsplit
import uuid

from beepgrid import Client

root = Path(__file__).resolve().parents[2]
url = os.environ.get('BEEPGRID_URL', 'http://localhost:4010')
host = urlsplit(url).hostname
if host != 'localhost' and not (host and ipaddress.ip_address(host).is_loopback):
    raise RuntimeError('This fixture only runs against a disposable loopback workspace')
path = Path(os.environ.get('BEEPGRID_TOKEN_FILE', root / '.local/owner-token'))
if path.stat().st_mode & 0o077:
    raise RuntimeError('Credential file must be owner-only: chmod 600')
owner = Client(url, path.read_text().strip())
me = owner.call('GET', '/api/v1/me')
if me.get('identity_type') != 'person' or not me.get('admin'):
    raise RuntimeError('Use a disposable local workspace owner credential')
person = me['person']['id']
run = uuid.uuid4().hex
peers = {}
for name in ('Builder', 'Reviewer', 'DevOps'):
    peers[name] = owner.connect('shared-example/' + run + '/' + name.lower(), name)
for name, (_, session) in peers.items():
    agent_id = session['agent']['id']
    contacts = [('person', person)] + [('agent', s['agent']['id']) for n, (_, s) in peers.items() if n != name]
    role = owner.call('POST', '/api/v1/roles', {
        'name': 'Shared conversation fixture ' + run + '/' + name,
        'rules': [{'action': action, 'target_kind': kind, 'target_id': contact}
                  for kind, contact in contacts for action in ('view', 'message', 'ask')]})
    owner.call('POST', '/api/v1/role-bindings', {
        'role_id': role['id'], 'subject_kind': 'principal', 'subject_id': agent_id})

with tempfile.TemporaryDirectory(prefix='beepgrid-shared-example-') as scratch:
    scratch = Path(scratch)
    credential = scratch / 'builder-token'
    credential.touch(mode=0o600)
    credential.write_text(peers['Builder'][1]['token'])
    binary = scratch / 'go-peer'
    subprocess.run(['go', 'build', '-buildvcs=false', '-o', str(binary), './examples/shared-conversation/go-peer'], cwd=root, check=True)

    def builder(operation, **fields):
        result = subprocess.run([str(binary)], input=json.dumps({'operation': operation, 'key': run, **fields}),
            text=True, capture_output=True, check=True, cwd=root,
            env={**os.environ, 'BEEPGRID_URL': url, 'BEEPGRID_TOKEN_FILE': str(credential)})
        return json.loads(result.stdout)

    chat = builder('start', person_id=person, reviewer_id=peers['Reviewer'][1]['agent']['id'])
    chat_id = chat['conversation_id']
    peers['Reviewer'][0].send(conversation_id=chat_id,
        body='Two scheduled jobs still use it. We need to move them first.', key=run+'/review')
    builder('invite', conversation_id=chat_id, devops_id=peers['DevOps'][1]['agent']['id'])
    peers['DevOps'][0].send(conversation_id=chat_id,
        body='I can migrate them to /v2/search. Keep v1 until the jobs drain.', key=run+'/devops')
    owner.send(conversation_id=chat_id,
        body='Go ahead. Keep the old endpoint until the migration is verified.', key=run+'/person')
    history = owner.conversation(chat_id)
    assert len(history['messages']) == 5
    assert {m['sender_id'] for m in history['messages']} == {person, *[s['agent']['id'] for _, s in peers.values()]}
    # DevOps sees the earlier messages only because Builder explicitly shared history.
    assert len(peers['DevOps'][0].conversation(chat_id)['messages']) == 5
    assert history['messages'][2]['mentions'][0]['principal_id'] == peers['DevOps'][1]['agent']['id']
    print('PASS: Go Builder, Python Reviewer/DevOps, explicit invitation, shared history and person reply.')
    print('Conversation:', chat_id)
