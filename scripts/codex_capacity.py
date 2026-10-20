"""Retry only an explicit Codex capacity rejection before any tool invocation."""
import json


def safe_capacity_retry(transcript: str) -> bool:
    events = []
    for line in transcript.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if isinstance(event, dict):
            events.append(event)
    if any(event.get('item', {}).get('type') == 'mcp_tool_call' for event in events):
        return False
    for event in events:
        if event.get('type') == 'error':
            message = event.get('message')
        elif event.get('type') == 'turn.failed':
            message = event.get('error', {}).get('message')
        else:
            continue
        if message == 'Selected model is at capacity. Please try a different model.':
            return True
    return False
