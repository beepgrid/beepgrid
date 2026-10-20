import json
import unittest

from codex_capacity import safe_capacity_retry


class CapacityRetryTests(unittest.TestCase):
    error = {'type': 'turn.failed', 'error': {'message': 'Selected model is at capacity. Please try a different model.'}}

    def test_explicit_pre_tool_capacity_rejection(self):
        self.assertTrue(safe_capacity_retry(json.dumps(self.error)))

    def test_started_tool_blocks_retry_even_without_completion(self):
        for event_type in ('item.started', 'item.completed'):
            started = {'type': event_type, 'item': {'type': 'mcp_tool_call'}}
            self.assertFalse(safe_capacity_retry(json.dumps(started) + '\n' + json.dumps(self.error)))

    def test_application_errors_and_model_prose_do_not_retry(self):
        self.assertFalse(safe_capacity_retry(json.dumps({'type': 'turn.failed', 'error': {'message': 'Unauthorized'}})))
        prose = {'type': 'item.completed', 'item': {'type': 'agent_message', 'text': self.error['error']['message']}}
        self.assertFalse(safe_capacity_retry(json.dumps(prose)))
        self.assertFalse(safe_capacity_retry('not JSON\nnull\n[]'))


if __name__ == '__main__':
    unittest.main()
