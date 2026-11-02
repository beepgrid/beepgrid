"""Use an existing worker credential, or explicitly enroll with NOTIF_AGENT_ID."""
import os
from pathlib import Path
from beepgrid import Client

token = os.environ.get("NOTIF_TOKEN", "")
if os.environ.get("NOTIF_TOKEN_FILE"):
    path = Path(os.environ["NOTIF_TOKEN_FILE"])
    if path.stat().st_mode & 0o077:
        raise RuntimeError("Credential file must be owner-only (chmod 600)")
    token = path.read_text().strip()
if not token:
    raise RuntimeError("Set NOTIF_TOKEN_FILE or NOTIF_TOKEN")
worker = Client(os.environ.get("NOTIF_URL", "http://localhost:4010"), token)
agent_id = os.environ.get("NOTIF_AGENT_ID", "").strip()
if agent_id:
    worker, session = worker.connect(agent_id, os.environ.get("NOTIF_AGENT_NAME", "Python worker"), ["message"])
    identity = {"identity_type": "agent", "agent": session["agent"], "instance_id": session["instance_id"]}
else:
    identity = worker.call("GET", "/api/v1/me")
if identity.get("identity_type") != "agent" or not identity.get("agent", {}).get("id") or not identity.get("instance_id"):
    raise RuntimeError("Without NOTIF_AGENT_ID, supply an existing worker credential for an agent connection")
print("Agent:", identity["agent"]["id"], "Instance:", identity["instance_id"], flush=True)

def handle(delivery):
    if delivery["payload"]["type"] == "message":
        message = delivery["payload"]["message"]
        worker.send(conversation_id=message["conversation_id"],
                    body="Python worker received: " + message["body"], key="reply/" + delivery["id"])

worker.run(handle)
