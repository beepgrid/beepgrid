"""Agent-only incident investigation using synthetic telemetry and a real local reproducer."""
from common import Fixture
from investigation_pattern import invite_and_ask

f = Fixture(['On-call', 'Deploy', 'Database', 'Checkout'])
oncall = f.client('On-call')
chat = oncall.start_chat(targets=[f.target('Deploy'), f.target('Database')],
    title='Checkout latency investigation', body='Checkout latency rose after 10:00. Compare recent changes.',
    key=f.run + '/room')
chat_id = chat['conversation_id']
f.chats.append(chat_id)

# In a real runtime these handlers query its deploy history and database traces.
questions = f.ask_each('On-call', ['Deploy', 'Database'], {'since': '10:00', 'service': 'checkout'}, 'evidence')
f.handle('Deploy', lambda q: {'service': q['service'], 'revision': 'r42', 'deployed_at': '10:00'})
f.handle('Database', lambda q: {'service': q['service'], 'revision': 'r42', 'query': 'cart_lookup', 'index_used': False})
evidence = f.answers('On-call', questions)
assert {r['revision'] for r in evidence} == {'r42'}
assert evidence[1]['query'] == 'cart_lookup' and not evidence[1]['index_used']

# The directory returns permitted contacts. Match a stable identity, not just a name.
checkout = next(a for a in oncall.directory('agent', 'Checkout')['items']
                if a['external_id'] == f.external_ids['Checkout'])
history = oncall.conversation(chat_id)
specialist = {'kind': 'agent', 'id': checkout['id']}
question = {
    'summary': 'Compare the recent revisions for the suspected lookup regression.',
    'revision': 'r42', 'query': 'cart_lookup'}
saved = invite_and_ask(oncall, chat_id, specialist, question, history['version'],
    f.run + '/specialist', share_history=True)
assert invite_and_ask(oncall, chat_id, specialist, question, history['version'],
    f.run + '/specialist', share_history=True) == saved
check = [{'id': saved['request_id']}]
f.requests.extend(check)


def reproduce(_):
    # Test the suspected algorithm change locally; no production query or rollback.
    carts = [{'id': n, 'total_cents': 1250} for n in range(20000)]
    index = {cart['id']: cart for cart in carts}
    target, visited = len(carts) - 1, 0
    for cart in carts:  # r42's accidental full scan
        visited += 1
        if cart['id'] == target:
            scanned = cart
            break
    indexed = index[target]  # candidate fix restores the indexed lookup
    assert indexed == scanned
    return {'same_result': True, 'rows_before': visited, 'rows_after': 1,
            'finding': 'The local reproducer confirms a scan regression.',
            'proposal': 'Restore the indexed lookup; validate against real traces before rollout.'}


result = f.handle('Checkout', reproduce)
assert result['rows_before'] == 20000 and result['rows_after'] == 1
f.client('Checkout').send(conversation_id=chat_id,
    body='Local reproducer: 20,000 rows scanned versus one indexed lookup, with the same result. Restore indexed lookup and verify real traces.',
    key=f.run + '/finding')
assert f.answers('On-call', check)[0]['same_result']
assert len(f.client('Checkout').conversation(chat_id)['messages']) >= 2
oncall.send(conversation_id=chat_id,
    body='Evidence points to r42 cart_lookup. The local reproducer supports an indexed-lookup fix; no production change has run.',
    key=f.run + '/summary')

f.prove_agent_only()
print('PASS: agent-only evidence collection, specialist discovery, explicit history sharing and local regression check.')
print('Conversation:', chat_id)
