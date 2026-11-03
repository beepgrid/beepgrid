"""Exercise all advertised MCP tools against the same real-engine tour fixture."""
import base64
import json
import os
from pathlib import Path
import runpy
import subprocess
import tempfile
import uuid

state = runpy.run_path('examples/developer-tour/main.py')
owner, builder, billing = (state[name] for name in ['owner', 'builder', 'billing'])
builder_id, billing_id, person_id = (state[name] for name in ['builder_id', 'billing_id', 'person_id'])
run_id = uuid.uuid4().hex
binary = os.getenv('BEEPGRID_DOC_BINARY', '.local/beepgrid')
called = set()

with tempfile.TemporaryDirectory(prefix='beepgrid-mcp-tour-') as scratch:
    builder_file, billing_file = [Path(scratch) / name for name in ['builder-token', 'billing-token']]
    for file, session in [(builder_file, state['builder_session']), (billing_file, state['billing_session'])]:
        file.touch(mode=0o600)
        file.write_text(session['token'])

    def rpc(name, arguments, credential=builder_file):
        env = {**os.environ, 'BEEPGRID_TOKEN': '', 'BEEPGRID_TOKEN_FILE': str(credential), 'BEEPGRID_AGENT_ID': ''}
        request = {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/call', 'params': {'name': name, 'arguments': arguments}}
        process = subprocess.run([binary, 'mcp'], input=json.dumps(request) + '\n', text=True,
                                 capture_output=True, env=env, check=True, timeout=20)
        result = json.loads(process.stdout)['result']
        assert not result['isError'], (name, result)
        called.add(name)
        return json.loads(result['content'][0]['text'])

    # region directory
    assert rpc('list_directory', {'kind': 'agent'})['items']
    assert rpc('list_agents', {})
    assert isinstance(rpc('request_status', {}), list)
    # endregion directory

    # region messages
    sent = rpc('send', {'target': {'kind': 'agent', 'id': billing_id}, 'body': 'Please check the staging ledger.', 'idempotency_key': run_id + '/send'})
    assert rpc('get_conversation', {'id': sent['conversation_id']})['messages']
    assert rpc('start_chat', {'targets': [{'kind': 'agent', 'id': billing_id}], 'body': 'Let us reconcile these orders.', 'idempotency_key': run_id + '/chat'})['conversation_id']
    # endregion messages

    # region membership
    chat = owner.start_chat(targets=[{'kind': 'agent', 'id': builder_id}], body='Review staging together.')
    current = owner.conversation(chat['conversation_id'])
    assert rpc('change_chat_membership', {'conversation_id': chat['conversation_id'], 'operation': 'rename',
        'version': current['version'], 'title': 'Staging review', 'idempotency_key': run_id + '/rename'},
        Path(os.environ['BEEPGRID_TOKEN_FILE']))['version'] > current['version']
    # endregion membership

    # region files
    encoded = base64.b64encode(b'Payment ledger fixture').decode()
    draft = rpc('stage_attachment', {'file_name': 'ledger.txt', 'media_type': 'text/plain', 'base64': encoded})
    with_file = builder.send(target={'kind': 'agent', 'id': billing_id}, body='Review the attached ledger.', attachment_ids=[draft['id']])
    uploaded = rpc('upload_attachment', {'conversation_id': with_file['conversation_id'], 'file_name': 'notes.txt', 'media_type': 'text/plain', 'base64': encoded})
    assert len(rpc('list_attachments', {'conversation_id': with_file['conversation_id']})) == 2
    assert rpc('download_attachment', {'id': uploaded['id']})['base64'] == encoded
    # endregion files

    # region workers
    assert rpc('report_progress', {'progress': {'phase': 'reconciling'}}, billing_file)['ok']
    for delivery in rpc('receive', {}, billing_file):
        assert rpc('acknowledge', {'id': delivery['id'], 'generation': delivery['generation']}, billing_file)['received']
    # endregion workers

    # region decisions
    request = rpc('ask', {'target': {'kind': 'agent', 'id': billing_id}, 'body': 'Which region hosts staging?', 'idempotency_key': run_id + '/ask'})
    current = rpc('get_request', {'id': request['id']}, billing_file)
    answered = rpc('reply', {'id': request['id'], 'version': current['version'], 'outcome': 'answered', 'answer': 'eu-west-1'}, billing_file)
    assert answered['state'] == 'resolved'
    pending = builder.ask(target={'kind': 'agent', 'id': billing_id}, body='I need the latest ledger.')
    assert rpc('set_blocking', {'id': pending['id'], 'blocking': True})['blocking']
    revised = rpc('replace_request', {'id': pending['id'], 'version': pending['version'], 'body': 'I need the ledger for orders 241 and 242.', 'idempotency_key': run_id + '/replace'})
    assert revised['replacement']['state'] == 'open'
    # endregion decisions

    # region anycast
    role = owner.call('POST', '/api/v1/groups', {'name': run_id + '/billing-role', 'purpose': 'agent_role', 'members': [billing_id]})
    visibility = owner.call('POST', '/api/v1/roles', {'name': run_id + '/role-discovery', 'rules': [{'action': 'view', 'target_kind': 'group', 'target_id': role['id']}]})
    owner.call('POST', '/api/v1/role-bindings', {'role_id': visibility['id'], 'subject_kind': 'principal', 'subject_id': builder_id})
    assert any(item['id'] == role['id'] for item in rpc('list_agent_roles', {})['items'])
    billing.heartbeat(anycast={'capacity': 1})
    dispatch = rpc('anycast', {'role_id': role['id'], 'kind': 'question', 'body': 'Which region handles billing?', 'idempotency_key': run_id + '/anycast'})
    assert dispatch['agent_id'] == billing_id
    observed = rpc('get_anycast', {'id': dispatch['id']})
    assert observed['request_id'] == dispatch['request_id']
    billing.respond(dispatch['request_id'], 'answered', answer='eu-west-1', version=1)
    # endregion anycast

    # region execution
    action = {'name': 'fixture', 'executor_id': builder_id, 'resource': 'staging', 'resource_version': 'v2', 'operation_key': run_id + '/fixture', 'arguments': {}}
    approval = owner.ask(target={'kind': 'person', 'id': person_id}, body='Approve this fixture?', kind='approval', action=action)
    owner.respond(approval['id'], 'approved', version=approval['version'])
    execution = rpc('redeem_approval', {'id': approval['id'], 'action': action})
    assert rpc('report_execution', {'id': execution['id'], 'state': 'succeeded', 'result': {'fixture': True}})['state'] == 'succeeded'
    # endregion execution

listing = subprocess.run([binary, 'mcp'], input=json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}) + '\n', text=True, capture_output=True, env={**os.environ, 'BEEPGRID_TOKEN': 'schema-only', 'BEEPGRID_AGENT_ID': ''}, check=True)
expected = {item['name'] for item in json.loads(listing.stdout)['result']['tools']}
assert called == expected, (called, expected)
print(f'PASS: all {len(called)} MCP tools with actual canonical results')
