#!/usr/bin/env python3
"""Two-step open-browser revocation drill in the isolated loopback workspace.

Run once to seed, sign in using the protected fixture, open the conversation,
then run --revoke. The browser must remove that context on revalidation. No
production tenant, provider or customer data is involved.
"""
import argparse
import json
import pathlib
import urllib.error
import urllib.request
import uuid

root = pathlib.Path(__file__).resolve().parents[2]
base = 'http://127.0.0.1:4040'
owner = (root / '.local/owner-token').read_text().strip()
fixture = root / '.local/workspace-revocation-fixture.json'
parser = argparse.ArgumentParser()
parser.add_argument('--revoke', action='store_true')
args = parser.parse_args()


def call(path, body=None, token=owner, expect=200):
    req = urllib.request.Request(base + '/api/v1' + path,
        data=json.dumps(body).encode() if body is not None else None,
        headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json',
                 'Idempotency-Key': uuid.uuid4().hex})
    try:
        with urllib.request.urlopen(req, timeout=15) as response:
            status, value = response.status, json.load(response)
    except urllib.error.HTTPError as error:
        status, value = error.code, json.load(error)
    assert status == expect, (path, status)
    return value


if args.revoke:
    data = json.loads(fixture.read_text())
    binding = data['binding']
    call('/role-bindings', {**binding, 'remove': True})
    call('/conversations/' + data['conversation_id'], token=data['token'], expect=404)
    call('/messages', {'conversation_id': data['conversation_id'], 'body': 'Must fail after revocation'}, token=data['token'], expect=404)
    print('Contact grant revoked. Current conversation read and send both denied.')
else:
    suffix = uuid.uuid4().hex[:8]
    person = call('/people', {'name': 'Morgan access check', 'external_id': 'workspace-revocation/' + suffix})
    worker = call('/sessions', {'agent': 'workspace-revocation/' + suffix, 'name': 'Revocation probe', 'capabilities': ['message']})
    role = call('/roles', {'name': 'Revocable contact ' + suffix, 'rules': [
        {'action': a, 'target_kind': 'agent', 'target_id': worker['agent']['id']} for a in ['view', 'message']]})
    binding = {'role_id': role['id'], 'subject_kind': 'principal', 'subject_id': person['id']}
    call('/role-bindings', binding)
    token = call('/credentials', {'principal_id': person['id'], 'kind': 'api', 'prefix': ''})['token']
    message = call('/messages', {'target': {'kind': 'agent', 'id': worker['agent']['id']},
        'body': 'Synthetic context for open-page revocation.', 'title': 'Revocation qualification'}, token=token)
    fixture.write_text(json.dumps({'person': person, 'binding': binding, 'token': token, 'conversation_id': message['conversation_id']}))
    fixture.chmod(0o600)
    print('Seeded protected fixture:', fixture.relative_to(root))
