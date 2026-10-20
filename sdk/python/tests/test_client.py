import json
import os
import unittest
import uuid
from unittest.mock import patch
from notif import Client, NotifError


class ClientTests(unittest.TestCase):
    def test_timeout_does_not_cancel_request(self):
        c = Client("http://localhost", "token")
        with patch.object(c, "get_request", return_value={"state": "open"}) as query:
            with self.assertRaises(TimeoutError):
                c.wait("req_1", timeout=.01, interval=.001)
            self.assertGreater(query.call_count, 0)

    def test_invalid_base_url(self):
        with self.assertRaises(ValueError):
            Client("file:///etc/passwd", "token")

    def test_explicit_idempotency_key(self):
        c = Client("http://localhost", "token")
        with patch.object(c, "call", return_value={"id": "req_1"}) as call:
            c.ask(target={"kind": "person", "id": "alice"}, title="Continue?", key="stable")
            self.assertEqual(call.call_args.kwargs["key"], "stable")

    @unittest.skipUnless(os.getenv("NOTIF_SDK_TEST_URL"), "Real HTTP SDK test requires NOTIF_SDK_TEST_URL")
    def test_real_worker_transport(self):
        owner = Client(os.environ["NOTIF_SDK_TEST_URL"], os.environ["NOTIF_SDK_TEST_TOKEN"])
        worker, session = owner.connect("sdk/python-" + str(uuid.uuid4()), "Python worker", ["message"])
        owner.send(body="Hello from Python", target={"kind": "agent", "id": session["agent"]["id"]})
        messages = worker.inbox()
        self.assertEqual(len(messages), 1)
        self.assertEqual(messages[0]["payload"]["message"]["body"], "Hello from Python")
        worker.ack(messages[0])


if __name__ == "__main__":
    unittest.main()
