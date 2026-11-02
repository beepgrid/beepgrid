"""Two numbered, authenticated agents exchange a file through a disposable test workspace.

Run only with an explicitly selected test workspace's owner credential. The owner
creates the minimum test contact grants; all communication uses worker credentials.
No model or external provider is called. No automatic business decision is made.
"""
import hashlib
import os
import pathlib
import time
import uuid
from beepgrid import Client


def token():
    file = os.getenv("NOTIF_TOKEN_FILE")
    if file:
        path = pathlib.Path(file)
        if path.stat().st_mode & 0o077:
            raise RuntimeError("Credential file must be owner-only (chmod 600)")
        return path.read_text().strip()
    return os.environ["NOTIF_TOKEN"]


owner = Client(os.environ["NOTIF_URL"], token())
run_id = uuid.uuid4().hex[:12]
first, first_session = owner.connect("file-demo/" + run_id + "/1", "Reviewer 1")
second, second_session = owner.connect("file-demo/" + run_id + "/2", "Reviewer 2")
first_id, second_id = first_session["agent"]["id"], second_session["agent"]["id"]
for source, target in [(first_id, second_id), (second_id, first_id)]:
    role = owner.call("POST", "/api/v1/roles", {"name": "File demo " + run_id + " " + source,
        "rules": [{"action": action, "target_kind": "agent", "target_id": target} for action in ["view", "message"]]})
    owner.call("POST", "/api/v1/role-bindings", {"role_id": role["id"], "subject_kind": "principal", "subject_id": source})
sent = first.send(target={"kind": "agent", "id": second_id}, body="Please review the attached brief.", key=run_id + "/start")
file_bytes = b"%PDF-1.4\n% Beep Grid deterministic file transport fixture\n%%EOF\n"
file = first.upload_attachment(sent["conversation_id"], "brief.pdf", "application/pdf", file_bytes)
first.send(conversation_id=sent["conversation_id"], body="Review file " + file["id"], key=run_id + "/file-ready")
finished = False
expected = hashlib.sha256(file_bytes).hexdigest()


def handle(delivery):
    global finished
    payload = delivery["payload"]
    if payload["type"] != "message" or payload["message"]["body"] != "Review file " + file["id"]:
        return
    # File access is checked again using Reviewer 2's current credentials.
    data = second.download_attachment(file["id"])
    observed = hashlib.sha256(data).hexdigest()
    if observed != expected:
        raise RuntimeError("File integrity mismatch")
    second.send(conversation_id=sent["conversation_id"], body="Reviewed brief.pdf: " + observed,
                key="file-review-reply/" + delivery["id"])
    # A crash before transport acknowledgement replays the same stable reply key.
    finished = True


second.run(handle, stop=lambda: finished)
deadline = time.monotonic() + 10
while time.monotonic() < deadline:
    deliveries = first.inbox()
    for delivery in deliveries:
        payload = delivery["payload"]
        first.ack(delivery)
        if payload["type"] == "message" and payload["message"]["body"] == "Reviewed brief.pdf: " + expected:
            print("PASS: Reviewer 1 and Reviewer 2 exchanged a verified file and canonical reply.")
            raise SystemExit(0)
    time.sleep(.1)
raise RuntimeError("Reply not observed before the local deadline")
