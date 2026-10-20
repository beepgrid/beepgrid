"""A deterministic transport example. The caller's launcher role is inherited on first enrollment."""
import os
from notif import Client

worker, session = Client(os.environ["NOTIF_URL"], os.environ["NOTIF_TOKEN"]).connect(
    "examples/python", "Python worker", ["message"])
print("Agent:", session["agent"]["id"])

def handle(delivery):
    if delivery["payload"]["type"] == "message":
        message = delivery["payload"]["message"]
        worker.send(conversation_id=message["conversation_id"],
                    body="Python worker received: " + message["body"], key="reply/" + delivery["id"])

worker.run(handle)
