import asyncio
import os
import tempfile
import unittest
from beepgrid.runtime import LiveInbox, input_text


class RuntimeTests(unittest.IsolatedAsyncioTestCase):
    async def test_custody_dedup_and_identity(self):
        with tempfile.TemporaryDirectory() as folder:
            path = folder + "/inbox.db"
            inbox = LiveInbox(None, path, "workspace/instance")
            job = {
                "id": "del_one",
                "generation": 1,
                "payload": {
                    "type": "message",
                    "conversation_id": "con_a",
                    "message": {"sender_id": "agent_peer", "body": "Keep V1"},
                },
            }
            inbox.put(job)
            inbox.put({**job, "generation": 2})
            self.assertEqual(len(inbox.pending()), 1)
            self.assertIn("agent_peer", input_text(inbox.pending()[0]))
            with self.assertRaisesRegex(ValueError, "already has"):
                LiveInbox(None, path, "workspace/instance")
            await inbox.close()
            with self.assertRaisesRegex(ValueError, "another connection"):
                LiveInbox(None, path, "different")
            recovered = LiveInbox(None, path, "workspace/instance")
            self.assertEqual(recovered.pending()[0]["id"], "del_one")
            recovered.checkpoint(["del_one"])
            self.assertEqual(recovered.pending(), [])
            self.assertEqual(os.stat(path).st_mode & 0o777, 0o600)
            await recovered.close()

    async def test_ack_follows_durable_custody_and_revocation_stops(self):
        with tempfile.TemporaryDirectory() as folder:
            calls = []

            class Client:
                def call(self, *args):
                    if calls:
                        raise PermissionError("revoked")
                    return [
                        {
                            "id": "one",
                            "generation": 1,
                            "payload": {"type": "message", "conversation_id": "c"},
                        }
                    ]

                def ack(self, delivery):
                    calls.append(delivery["id"])

            inbox = LiveInbox(Client(), folder + "/inbox.db", "worker").start()
            with self.assertRaises(PermissionError):
                await inbox.receiving
            self.assertEqual(calls, ["one"])
            self.assertEqual(inbox.pending()[0]["id"], "one")
            await inbox.close()
