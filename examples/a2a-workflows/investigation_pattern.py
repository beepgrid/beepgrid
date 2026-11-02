"""Bring another specialist into an investigation and track its follow-up separately."""
import json


def invite_and_ask(client, conversation_id, specialist, question, version, key,
                   *, share_history=False):
    """Persist these inputs, including version, before submitting the membership change."""
    client.change_chat_membership(conversation_id, operation='add', version=version,
        members=[specialist['id']], share_history=share_history, key=key + '/invite')
    client.send(conversation_id=conversation_id, body=question['summary'], key=key + '/context')
    request = client.ask(target=specialist, body=json.dumps(question, sort_keys=True), key=key + '/question')
    return {'conversation_id': conversation_id, 'request_id': request['id']}
