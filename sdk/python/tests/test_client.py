import json
import os
import unittest
import uuid
from unittest.mock import patch
from notif import Client, NotifError


class ClientTests(unittest.TestCase):
    def test_identified_client_works_behind_a_gateway_rejecting_generic_urllib(self):
        import http.server
        import threading
        class Gateway(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args): pass
            def do_GET(self):
                if self.headers.get("User-Agent", "").startswith("Python-urllib"):
                    self.send_response(403); self.end_headers(); return
                self.send_response(200); self.end_headers(); self.wfile.write(b'{"reachable":true}')
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Gateway)
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        try:
            import urllib.error
            import urllib.request
            url = "http://127.0.0.1:" + str(server.server_port)
            with self.assertRaises(urllib.error.HTTPError) as error:
                urllib.request.urlopen(url + "/api/v1/me")
            self.assertEqual(error.exception.code, 403)
            error.exception.close()
            self.assertEqual(Client(url, "fixture").call("GET", "/api/v1/me"), {"reachable":True})
        finally:
            server.shutdown(); server.server_close(); thread.join()

    def test_timeout_does_not_cancel_request(self):
        c = Client("http://localhost", "token")
        with patch.object(c, "get_request", return_value={"state": "open"}) as query:
            with self.assertRaises(TimeoutError):
                c.wait("req_1", timeout=.01, interval=.001)
            self.assertGreater(query.call_count, 0)

    def test_assistance_helpers_preserve_authority_and_provider_deadline(self):
        c = Client("http://localhost", "person-token")
        with patch.object(c, "call", return_value={"id": "ovr_1"}) as call:
            c.assistance()
            call.assert_called_with("GET", "/api/v1/assistance/settings")
            c.update_assistance(overseer=True)
            call.assert_called_with("POST", "/api/v1/assistance/settings", {"overseer": True})
            c.plan_overseer("Ask Scout")
            call.assert_called_with("POST", "/api/v1/fleet/plan", {"prompt": "Ask Scout"}, timeout=40)
            c.plan_overseer("Follow up", previous_plan_id="ovr_1", key="turn-2")
            call.assert_called_with("POST", "/api/v1/fleet/plan", {"prompt": "Follow up", "previous_plan_id": "ovr_1"}, timeout=40, key="turn-2")
            c.confirm_overseer("ovr_1")
            call.assert_called_with("POST", "/api/v1/fleet/command", {"plan_id": "ovr_1"})
            c.overseer_plan("a/b")
            call.assert_called_with("GET", "/api/v1/fleet/plans/a%2Fb")
            c.overseer_plans()
            call.assert_called_with("GET", "/api/v1/fleet/plans")
            c.summarize_overseer("a/b")
            call.assert_called_with("POST", "/api/v1/fleet/plans/a%2Fb/summary", {}, timeout=40)

    def test_invalid_base_url(self):
        with self.assertRaises(ValueError):
            Client("file:///etc/passwd", "token")

    def test_explicit_idempotency_key(self):
        c = Client("http://localhost", "token")
        with patch.object(c, "call", return_value={"id": "req_1"}) as call:
            c.ask(target={"kind": "person", "id": "alice"}, title="Continue?", key="stable")
            self.assertEqual(call.call_args.kwargs["key"], "stable")

    def test_lost_response_body_retries_same_operation_key(self):
        import http.client
        class Response:
            def __init__(self, broken): self.broken = broken
            def __enter__(self): return self
            def __exit__(self, *args): return False
            def read(self):
                if self.broken: raise http.client.IncompleteRead(b'{')
                return b'{"id":"request-1"}'
        calls = []
        def transport(request, **kwargs):
            calls.append(request.get_header("Idempotency-key"))
            return Response(len(calls)==1)
        with patch("urllib.request.urlopen", side_effect=transport):
            self.assertEqual(Client("http://localhost","token").ask(target={"kind":"person","id":"p"},title="Continue?",key="saved-key")["id"],"request-1")
        self.assertEqual(calls,["saved-key","saved-key"])

    @unittest.skipUnless(os.getenv("NOTIF_SDK_TEST_URL"), "Real HTTP SDK test requires NOTIF_SDK_TEST_URL")
    def test_real_compact_receipt_and_full_replay(self):
        owner = Client(os.environ["NOTIF_SDK_TEST_URL"], os.environ["NOTIF_SDK_TEST_TOKEN"])
        _, session = owner.connect("sdk/python-receipt-" + str(uuid.uuid4()), "Receipt worker")
        options = dict(body="Receipt body", target={"kind": "agent", "id": session["agent"]["id"]}, key=str(uuid.uuid4()))
        receipt = owner.notify(**options)
        self.assertEqual(receipt["message_count"], 1)
        self.assertNotIn("messages", receipt)
        full = owner.send(**options)
        self.assertEqual(full["conversation_id"], receipt["conversation_id"])
        self.assertEqual(full["messages"][0]["body"], "Receipt body")

    @unittest.skipUnless(os.getenv("NOTIF_SDK_TEST_URL"), "Real HTTP SDK test requires NOTIF_SDK_TEST_URL")
    def test_real_worker_transport(self):
        owner = Client(os.environ["NOTIF_SDK_TEST_URL"], os.environ["NOTIF_SDK_TEST_TOKEN"])
        worker, session = owner.connect("sdk/python-" + str(uuid.uuid4()), "Python worker", ["message"])
        owner.send(body="Hello from Python", target={"kind": "agent", "id": session["agent"]["id"]})
        messages = worker.inbox()
        self.assertEqual(len(messages), 1)
        self.assertEqual(messages[0]["payload"]["message"]["body"], "Hello from Python")
        worker.ack(messages[0])

    @unittest.skipUnless(os.getenv("NOTIF_SDK_TEST_URL"), "Real HTTP SDK test requires NOTIF_SDK_TEST_URL")
    def test_real_approval_execution_lifecycle(self):
        owner = Client(os.environ["NOTIF_SDK_TEST_URL"], os.environ["NOTIF_SDK_TEST_TOKEN"])
        me = owner.call("GET", "/api/v1/me")
        worker, session = owner.connect("sdk/python-exec-"+str(uuid.uuid4()), "Python executor")
        action = {"name":"fixture", "executor_id":session["agent"]["id"], "resource":"fixture", "resource_version":"1", "arguments":{"value":1}, "operation_key":str(uuid.uuid4())}
        q = owner.ask(kind="approval", title="Fixture?", target={"kind":"person","id":me["person"]["id"]}, action=action)
        with self.assertRaises(NotifError) as raised:
            worker.redeem(q["id"], action)
        self.assertEqual(raised.exception.status,409)
        owner.respond(q["id"],"approved",version=q["version"])
        execution=worker.redeem(q["id"], action)
        self.assertEqual(worker.redeem(q["id"],action)["id"],execution["id"])
        with self.assertRaises(NotifError) as raised:
            worker.redeem(q["id"],{**action,"resource_version":"2"})
        self.assertEqual(raised.exception.status,403)
        self.assertEqual(worker.report_execution(execution["id"],"succeeded",{"reported":True})["state"],"succeeded")
        with self.assertRaises(NotifError) as raised:
            worker.report_execution(execution["id"],"failed")
        self.assertEqual(raised.exception.status,409)

    @unittest.skipUnless(os.getenv("NOTIF_SDK_TEST_URL"), "Requires real HTTP")
    def test_real_workspace_queries_and_batches(self):
        c = Client(os.environ["NOTIF_SDK_TEST_URL"], os.environ["NOTIF_SDK_TEST_TOKEN"])
        _, session = c.connect("sdk/query-python-" + str(uuid.uuid4()), "Query worker")
        target = {"kind": "agent", "id": session["agent"]["id"]}
        preview = c.dispatch_preview([target], "message")
        sent = c.send_batch([target, target], "one", preview_token=preview["token"])
        self.assertEqual(len(sent), 1)
        page = c.query("messages", filters={"conversation_id": sent[0]["result"]["conversation_id"]}, limit=1)
        self.assertEqual(len(page["items"]), 1)
        requests = c.ask_batch([target], {"title": "Progress?"})
        self.assertTrue(c.capabilities(request_id=requests[0]["request"]["id"])["operations"]["cancel"])
        self.assertEqual(c.request_trace(requests[0]["request"]["id"])["request"]["id"], requests[0]["request"]["id"])
        self.assertEqual(c.workspace()["deployment"], "self_hosted")
        self.assertIn("applied", c.account())
        self.assertIn("roles", c.access())


if __name__ == "__main__":
    unittest.main()
