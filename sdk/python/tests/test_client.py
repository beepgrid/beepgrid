import json
import os
import unittest
import uuid
from unittest.mock import patch
from notif import Client, NotifError


class ClientTests(unittest.TestCase):
    @unittest.skipUnless(os.getenv("NOTIF_SDK_TEST_URL"), "Requires real HTTP")
    def test_recipient_preferences_preserve_inheritance_and_reject_stale_edits(self):
        c = Client(os.environ["NOTIF_SDK_TEST_URL"], os.environ["NOTIF_SDK_TEST_TOKEN"])
        person = c.call("GET", "/api/v1/me")["person"]["id"]
        before = c.delivery_preferences(person)
        saved = c.save_delivery_preferences(person, {**before, "default": {"mode": "preferred", "destinations": [], "followup_seconds": 0}})
        self.assertEqual(saved["version"], before["version"] + 1)
        self.assertEqual(c.delivery_preferences(person)["default"]["destinations"], [])
        with self.assertRaises(NotifError) as error:
            c.save_delivery_preferences(person, before)
        self.assertEqual(error.exception.status, 409)
        restored = c.save_delivery_preferences(person, {**before, "version": saved["version"]})
        self.assertEqual(restored["default"], before["default"])

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
        with patch("notif._open", side_effect=transport):
            self.assertEqual(Client("http://localhost","token").ask(target={"kind":"person","id":"p"},title="Continue?",key="saved-key")["id"],"request-1")
        self.assertEqual(calls,["saved-key","saved-key"])

    @unittest.skipUnless(os.getenv("NOTIF_SDK_TEST_URL"), "Real HTTP SDK test requires NOTIF_SDK_TEST_URL")
    def test_real_question_in_existing_chat_without_redundant_target(self):
        owner = Client(os.environ["NOTIF_SDK_TEST_URL"], os.environ["NOTIF_SDK_TEST_TOKEN"])
        person = owner.call("GET", "/api/v1/me")["person"]["id"]
        worker, session = owner.connect("sdk/python-followup-" + str(uuid.uuid4()), "Follow-up worker")
        target = {"kind": "agent", "id": session["agent"]["id"]}
        owner.save_contact_permissions(target, [{"action": "view", "target_kind": "person", "target_id": person}], version=0)
        chat = owner.send(target=target, body="Review the release")
        question = owner.ask(conversation_id=chat["conversation_id"], body="Which version?", key=str(uuid.uuid4()))
        self.assertEqual(question["conversation_id"], chat["conversation_id"])
        worker.respond(question["id"], "answered", version=question["version"], answer="v2")
        self.assertEqual(owner.get_request(question["id"])["response"]["answer"], "v2")

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
        owner = c.call("GET", "/api/v1/me")
        before = c.preview_contact_permissions(target)
        c.save_contact_permissions(target, [{"action": "view", "target_kind": "person", "target_id": owner["person"]["id"]}], version=before["policy"]["version"])
        requests = c.ask_batch([target], {"title": "Progress?"})
        self.assertTrue(c.capabilities(request_id=requests[0]["request"]["id"])["operations"]["cancel"])
        self.assertEqual(c.request_trace(requests[0]["request"]["id"])["request"]["id"], requests[0]["request"]["id"])
        self.assertEqual(c.workspace()["deployment"], "self_hosted")
        self.assertIn("applied", c.account())
        self.assertIn("roles", c.access())


if __name__ == "__main__":
    unittest.main()

class FileJourneyTests(unittest.TestCase):
    @unittest.skipUnless(os.getenv("NOTIF_SDK_TEST_URL"), "Requires real HTTP")
    def test_files_events_and_current_isolation(self):
        owner = Client(os.environ["NOTIF_SDK_TEST_URL"], os.environ["NOTIF_SDK_TEST_TOKEN"])
        worker, session = owner.connect("sdk/python-files-" + str(uuid.uuid4()), "Python file worker")
        person = owner.call("GET", "/api/v1/me")["person"]["id"]
        role = owner.call("POST", "/api/v1/roles", {"name": "File reader " + str(uuid.uuid4()), "rules": [{"action": "view", "target_kind": "person", "target_id": person}]})
        owner.call("POST", "/api/v1/role-bindings", {"role_id": role["id"], "subject_kind": "principal", "subject_id": session["agent"]["id"]})
        other, _ = owner.connect("sdk/python-isolated-" + str(uuid.uuid4()), "Isolated worker")
        sent = owner.send(target={"kind": "agent", "id": session["agent"]["id"]}, body="Please review this PDF")
        file = owner.upload_attachment(sent["conversation_id"], "brief.pdf", "application/pdf", b"%PDF-1.4\nfixture")
        self.assertEqual(worker.attachments(sent["conversation_id"])[0]["id"], file["id"])
        self.assertEqual(worker.download_attachment(file["id"]), b"%PDF-1.4\nfixture")
        with self.assertRaises(NotifError): other.download_attachment(file["id"])
        self.assertGreaterEqual(len(owner.directory("person")["items"]), 1)
        stream = worker.events()
        try:
            for event in stream:
                if event["kind"] == "message.created" and event["conversation_id"] == sent["conversation_id"]:
                    self.assertEqual(event["data"]["body"], "Please review this PDF")
                    break
        finally:
            stream.close()

class StartChatTests(unittest.TestCase):
    def test_membership_preserves_key_and_defaults_to_history_from_join(self):
        client = Client("http://localhost", "fixture")
        with patch.object(client, "call", return_value={"version": 4}) as call:
            client.change_chat_membership("a/b", operation="add", version=3, members=["agent_2"], key="membership-1")
            call.assert_called_once_with("POST", "/api/v1/conversations/a%2Fb/membership",
                {"operation":"add","version":3,"members":["agent_2"]}, key="membership-1")
        with patch.object(client, "call", return_value={"version": 5}) as call:
            client.change_chat_membership("a/b", operation="add", version=4, members=["agent_2"], share_history=True, key="membership-2")
            self.assertTrue(call.call_args.args[2]["share_history"])

    def test_stage_attachment_is_private_and_never_automatically_retried(self):
        client = Client("http://localhost", "fixture")
        with patch.object(client, "_file_request", return_value=b'{"id":"draft"}') as call:
            self.assertEqual(client.stage_attachment("brief.txt", "text/plain", b"private draft")["id"], "draft")
            self.assertEqual(call.call_args.args[:2], ("POST", "/api/v1/attachments"))
            self.assertIn(b"private draft", call.call_args.args[2])
        with patch.object(client, "_file_request", side_effect=ConnectionError("lost response")) as call:
            with self.assertRaises(ConnectionError): client.stage_attachment("brief.txt", "text/plain", b"private draft")
            self.assertEqual(call.call_count, 1)

    def test_start_chat_preserves_codepoint_offsets_choices_and_approval(self):
        client = Client("http://localhost", "fixture")
        mentions = [{"principal_id": "a", "start": 2, "end": 13}]
        with patch.object(client, "call", return_value={"conversation_id": "shared_1"}) as call:
            client.start_chat(targets=[{"kind":"agent","id":"a"}], body="👋 @Reviewer 2 please review", approval=True,
                              choices=["Yes","No"], mentions=mentions, attachment_ids=["draft"], key="shared-1")
            call.assert_called_once_with("POST", "/api/v1/conversations", {"targets":[{"kind":"agent","id":"a"}],
                "body":"👋 @Reviewer 2 please review","approval":True,"choices":["Yes","No"],"mentions":mentions,"attachment_ids":["draft"]}, key="shared-1")

    @unittest.skipUnless(os.getenv("NOTIF_SDK_TEST_URL") and os.getenv("NOTIF_SDK_EXPECT_START_CHAT_MENTIONS"), "Requires integrated StartChat mention engine")
    def test_real_start_chat_mentions(self):
        owner = Client(os.environ["NOTIF_SDK_TEST_URL"], os.environ["NOTIF_SDK_TEST_TOKEN"])
        worker, session = owner.connect("sdk/python-shared-" + str(uuid.uuid4()), "Reviewer 2")
        person = owner.call("GET", "/api/v1/me")["person"]["id"]
        role = owner.call("POST", "/api/v1/roles", {"name":"Shared reader " + str(uuid.uuid4()), "rules":[{"action":"view","target_kind":"person","target_id":person}]})
        owner.call("POST", "/api/v1/role-bindings", {"role_id":role["id"],"subject_kind":"principal","subject_id":session["agent"]["id"]})
        mentions = [{"principal_id":session["agent"]["id"],"start":2,"end":13}]
        sent = owner.start_chat(targets=[{"kind":"agent","id":session["agent"]["id"]}],body="👋 @Reviewer 2 please review",mentions=mentions,key=str(uuid.uuid4()))
        self.assertEqual(sent["messages"][0]["mentions"], mentions)
        self.assertTrue(any(d["payload"].get("message", {}).get("body") == "👋 @Reviewer 2 please review" for d in worker.inbox()))
