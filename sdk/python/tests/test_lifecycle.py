import os
import unittest
import uuid
from notif import Client, NotifError


@unittest.skipUnless(os.getenv("NOTIF_SDK_TEST_URL") and os.getenv("NOTIF_SDK_EXPECT_CHAT_LIFECYCLE"), "Requires integrated file and membership engine")
class SharedLifecycleTests(unittest.TestCase):
    def test_staged_files_and_membership_keep_default_history_and_current_access(self):
        owner = Client(os.environ["NOTIF_SDK_TEST_URL"], os.environ["NOTIF_SDK_TEST_TOKEN"])
        person = owner.call("GET", "/api/v1/me")["person"]["id"]
        members = [owner.connect("sdk/python-lifecycle/" + str(uuid.uuid4()), "Python lifecycle worker") for _ in range(3)]
        for member, session in members:
            rules = [{"action":"view", "target_kind":"person", "target_id":person}]
            rules += [{"action":"view", "target_kind":"agent", "target_id":peer["agent"]["id"]} for _, peer in members if peer is not session]
            role = owner.call("POST", "/api/v1/roles", {"name":"Lifecycle view " + str(uuid.uuid4()), "rules":rules})
            owner.call("POST", "/api/v1/role-bindings", {"role_id":role["id"], "subject_kind":"principal", "subject_id":session["agent"]["id"]})
        reader, session = members[2]
        before = owner.stage_attachment("before.txt", "text/plain", b"Before joining")
        with self.assertRaises(NotifError): reader.download_attachment(before["id"])
        chat = owner.start_chat(targets=[{"kind":"agent", "id":item["agent"]["id"]} for _, item in members[:2]], body="Before joining", attachment_ids=[before["id"]])
        self.assertEqual(chat["messages"][0]["attachments"][0]["id"], before["id"])
        changed = owner.change_chat_membership(chat["conversation_id"], operation="add", version=1, members=[session["agent"]["id"]])
        self.assertEqual(reader.conversation(chat["conversation_id"]).get("messages", []), [])
        with self.assertRaises(NotifError): reader.download_attachment(before["id"])
        after = owner.stage_attachment("after.txt", "text/plain", b"After joining")
        owner.send(conversation_id=chat["conversation_id"], body="After joining", attachment_ids=[after["id"]])
        self.assertEqual(reader.download_attachment(after["id"]), b"After joining")
        changed = owner.change_chat_membership(chat["conversation_id"], operation="remove", version=changed["version"], members=[session["agent"]["id"]])
        with self.assertRaises(NotifError): reader.download_attachment(after["id"])
        owner.change_chat_membership(chat["conversation_id"], operation="add", version=changed["version"], members=[session["agent"]["id"]], share_history=True)
        self.assertEqual(reader.download_attachment(before["id"]), b"Before joining")
