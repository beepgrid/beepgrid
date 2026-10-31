"""Agent-only API migration. Real HTTP and local contract checks; no model or deployment."""
import json

from common import Fixture
from review_pattern import start_review, review_status

f = Fixture(['API', 'Web', 'SDK'])
api = f.client('API')
reviewers = [f.target('Web'), f.target('SDK')]
proposal = {
    'title': 'Orders API migration', 'summary': 'Check the proposed response contract.',
    'revision': 'orders-v2/1', 'response': {'total_cents': 1250}}
saved = start_review(api, reviewers, proposal, f.run + '/first')
assert start_review(api, reviewers, proposal, f.run + '/first') == saved
chat_id = saved['conversation_id']
f.chats.append(chat_id)
first = [{'id': review['request_id']} for review in saved['reviews']]
f.requests.extend(first)
assert all(row['state'] == 'open' for row in review_status(api, saved))


def web_contract(task):
    proposal = task['proposal']
    # Replace this local parser with your web repository's contract test command.
    try:
        cents = proposal['response']['amount_cents']
        assert cents == 1250
        return {'compatible': True, 'consumer': 'web', 'field': 'amount_cents'}
    except KeyError:
        return {'compatible': False, 'consumer': 'web', 'missing': 'amount_cents'}


def sdk_contract(task):
    proposal = task['proposal']
    # The new SDK already reads the renamed field. Both fields use cents.
    assert proposal['response']['total_cents'] == 1250
    return {'compatible': True, 'consumer': 'sdk', 'field': 'total_cents'}


# Both requests are created before either specialist replies.
for name, check in [('Web', web_contract), ('SDK', sdk_contract)]:
    result = f.handle(name, check)
    f.client(name).send(conversation_id=chat_id, body=json.dumps(result),
                        key=f.run + '/first/' + name)
initial = f.answers('API', first)
assert all(row['state'] == 'resolved' for row in review_status(api, saved))
assert sorted(r['compatible'] for r in initial) == [False, True]
api.send(conversation_id=chat_id,
    body='Web still needs amount_cents. Keep it as an alias while consumers migrate.',
    key=f.run + '/compatibility')

# The API agent revises the proposal instead of pushing a breaking release.
second = f.ask_each('API', ['Web', 'SDK'], {'proposal': {'revision': 'orders-v2/2',
    'response': {'amount_cents': 1250, 'total_cents': 1250}}, 'conversation_id': chat_id}, 'check-v2')
for name, check in [('Web', web_contract), ('SDK', sdk_contract)]:
    result = f.handle(name, check)
    f.client(name).send(conversation_id=chat_id, body=json.dumps(result),
                        key=f.run + '/second/' + name)
assert all(r['compatible'] for r in f.answers('API', second))
api.send(conversation_id=chat_id,
    body='Both checks pass. Ship the additive response first; remove the alias only after Web migrates.',
    key=f.run + '/plan')

f.prove_agent_only()
print('PASS: agent-only contract checks caught a breaking change and verified the compatible revision.')
print('Conversation:', chat_id)
