"""A reusable proposal, parallel peer checks and durable result-collection pattern."""
import json


def start_review(client, reviewers, proposal, key):
    """Persist reviewers, proposal and key before calling; save the returned handles."""
    room = client.start_chat(targets=reviewers, title=proposal['title'],
        body=proposal['summary'], key=key + '/room')
    items = client.ask_batch(reviewers, {'body': json.dumps({
        'proposal': proposal, 'conversation_id': room['conversation_id']}, sort_keys=True)},
        key=key + '/reviews')
    return {'conversation_id': room['conversation_id'], 'reviews': [
        {'target': item['target'], 'request_id': item['request']['id']} for item in items]}


def review_status(client, saved):
    """Read current results without withdrawing or restarting pending work."""
    results = []
    for review in saved['reviews']:
        request = client.get_request(review['request_id'])
        results.append({'target': review['target'], 'state': request['state'],
                        'answer': (request.get('response') or {}).get('answer')})
    return results
