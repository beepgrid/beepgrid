"""Disposable local fixture setup. All workflow traffic uses agent credentials."""
import ipaddress
import json
import os
import time
from pathlib import Path
from urllib.parse import urlsplit
import uuid

from beepgrid import Client


class Fixture:
    def __init__(self, names):
        root = Path(__file__).resolve().parents[2]
        url = os.getenv('BEEPGRID_URL', 'http://localhost:4010')
        host = urlsplit(url).hostname
        if host != 'localhost' and not (host and ipaddress.ip_address(host).is_loopback):
            raise RuntimeError('Use a disposable loopback workspace for fixture provisioning')
        credential = Path(os.getenv('BEEPGRID_TOKEN_FILE', str(root / '.local/owner-token')))
        if credential.stat().st_mode & 0o077:
            raise RuntimeError('Credential file must be private: chmod 600')
        self._admin = Client(url, credential.read_text().strip())
        me = self._admin.call('GET', '/api/v1/me')
        if me.get('identity_type') != 'person' or not me.get('admin'):
            raise RuntimeError('Use the disposable workspace owner for fixture setup')
        self.run = uuid.uuid4().hex
        self.clients, self.ids, self.external_ids = {}, {}, {}
        self.requests, self.chats = [], []
        for name in names:
            external = 'a2a/' + self.run + '/' + name.lower()
            client, session = self._admin.connect(external, name)
            self.clients[name], self.ids[name], self.external_ids[name] = client, session['agent']['id'], external
        for name, agent_id in self.ids.items():
            role = self._admin.call('POST', '/api/v1/roles', {
                'name': self.run + '/' + name,
                'rules': [{'action': action, 'target_kind': 'agent', 'target_id': peer}
                          for peer in self.ids.values() if peer != agent_id
                          for action in ('view', 'message', 'ask')]})
            self._admin.call('POST', '/api/v1/role-bindings', {
                'role_id': role['id'], 'subject_kind': 'principal', 'subject_id': agent_id})

    def client(self, name):
        return self.clients[name]

    def target(self, name):
        return {'kind': 'agent', 'id': self.ids[name]}

    def ask_each(self, sender, recipients, body, step):
        items = self.client(sender).ask_batch(
            [self.target(name) for name in recipients],
            {'body': json.dumps(body)}, key=self.run + '/' + step)
        # Batch items carry their target and request. Do not depend on server order.
        by_target = {item['target']['id']: item['request'] for item in items}
        requests = [by_target[self.ids[name]] for name in recipients]
        assert len(requests) == len(recipients)
        self.requests.extend(requests)
        return requests

    def handle(self, name, check):
        """Consume this fixture worker's messages and answer its queued questions."""
        client = self.client(name)
        answered = []
        deadline = time.monotonic() + 5
        while not answered and time.monotonic() < deadline:
            deliveries = client.inbox()
            for delivery in deliveries:
                payload = delivery['payload']
                if payload['type'] == 'request':
                    request = client.get_request(payload['request']['id'])
                    if request['state'] == 'open':
                        result = check(json.loads(request['body']))
                        client.respond(request['id'], 'answered', version=request['version'], answer=result)
                        answered.append(result)
                # Ordered inboxes may deliver chat context before the question.
                # Consume that context, acknowledge it, then claim the next item.
                client.ack(delivery)
            if not deliveries:
                time.sleep(.05)
        assert answered, 'Expected a queued question for ' + name
        return answered[0]

    def answers(self, sender, requests):
        return [self.client(sender).wait(r['id'], timeout=5)['response']['answer'] for r in requests]

    def prove_agent_only(self):
        agent_ids = set(self.ids.values())
        for request in self.requests:
            saved = self._admin.get_request(request['id'])
            assert saved['requester_id'] in agent_ids and saved['resolved_by'] in agent_ids
            assert saved['state'] == 'resolved'
        for chat_id in self.chats:
            history = self._admin.conversation(chat_id)
            assert {p['id'] for p in history['participants']} <= agent_ids
            assert all(m['sender_id'] in agent_ids for m in history['messages'])
