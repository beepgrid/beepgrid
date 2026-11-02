#!/usr/bin/env python3
"""Qualify explicit enrollment and existing-worker modes against the SDK test engine."""
import os
import pathlib
import subprocess
import tempfile
import time
import uuid
from beepgrid import Client, BeepgridError

root = pathlib.Path(__file__).resolve().parents[1]
url = os.environ["BEEPGRID_SDK_TEST_URL"]
owner = Client(url, os.environ["BEEPGRID_SDK_TEST_TOKEN"])
person = owner.call("GET", "/api/v1/me")["person"]["id"]
binary = root / ".local/beepgrid-go-starter"
subprocess.run(["go", "build", "-buildvcs=false", "-o", str(binary), "./examples/go-worker"], cwd=root, check=True)
commands = [("Go", [str(binary)]), ("Python", ["python3", "examples/python-worker/main.py"]), ("TypeScript", ["node", "examples/typescript-worker/main.mjs"])]
role = owner.call("POST", "/api/v1/roles", {"name": "Starter contact " + uuid.uuid4().hex[:12],
    "rules": [{"action": action, "target_kind": "person", "target_id": person} for action in ["view", "message"]]})


def instances(agent_id):
    return owner.call("GET", "/api/v1/agents/" + agent_id + "/instances")


with tempfile.TemporaryDirectory(prefix="beepgrid-starter-") as directory:
    directory = pathlib.Path(directory)
    for language, command in commands:
        unique = uuid.uuid4().hex[:12]
        prefix = "sdk/starter/" + language.lower() + "-" + unique + "/"
        service = owner.call("POST", "/api/v1/services", {"external_id": "starter-launcher-" + unique, "name": language + " bounded launcher"})
        owner.call("POST", "/api/v1/role-bindings", {"role_id": role["id"], "subject_kind": "principal", "subject_id": service["id"]})
        issued = owner.call("POST", "/api/v1/credentials", {"principal_id": service["id"], "kind": "enrollment", "prefix": prefix})
        launcher = Client(url, issued["token"])
        try:
            launcher.connect("outside/" + unique, "Outside the permitted namespace")
        except BeepgridError as error:
            assert error.status == 403
        else:
            raise AssertionError("Launcher escaped its bounded namespace")

        for mode in ["enroll", "worker"]:
            logical = prefix + mode
            name = language + " " + mode + " " + unique
            decoy = None
            agent = None
            sent = None
            expected_instance = None
            expected_instances = None
            credential_value = issued["token"]
            if mode == "worker":
                # Same POST /sessions output as Connect UI: one already issued
                # worker token and its exact instance, with another replica to
                # prove logical-agent routing cannot hide accidental reenrollment.
                worker, session = launcher.connect(logical, name)
                decoy, decoy_session = launcher.connect(logical, name)
                agent = session["agent"]
                expected_instance = session["instance_id"]
                assert decoy_session["instance_id"] != expected_instance
                me = worker.call("GET", "/api/v1/me")
                assert me["identity_type"] == "agent" and me["instance_id"] == expected_instance and not me["admin"]
                credential_value = session["token"]
                expected_instances = {entry["id"] for entry in instances(agent["id"])}
                assert expected_instances == {expected_instance, decoy_session["instance_id"]}
                sent = owner.send(target={"kind": "instance", "id": expected_instance}, body="Starter transport test", key="starter/" + unique + "/" + mode)
                assert not any(d["payload"].get("conversation_id") == sent["conversation_id"] for d in decoy.inbox())
            credential = directory / (language.lower() + "-" + mode + "-token")
            credential.touch(mode=0o600)
            credential.write_text(credential_value)
            # The starter gets the UI's exact URL + protected token-file shape.
            # Do not pass the test owner's credential or any inherited BEEPGRID_ vars.
            environment = {key: value for key, value in os.environ.items() if not key.startswith("BEEPGRID_")}
            environment.update(BEEPGRID_URL=url, BEEPGRID_TOKEN_FILE=str(credential), PYTHONPATH=str(root / "sdk/python"))
            if mode == "enroll":
                environment.update(BEEPGRID_AGENT_ID=logical, BEEPGRID_AGENT_NAME=name)
            process = subprocess.Popen(command, cwd=root, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                if mode == "enroll":
                    deadline = time.monotonic() + 15
                    while time.monotonic() < deadline:
                        if process.poll() is not None:
                            raise RuntimeError(language + " enrollment starter exited before connection")
                        agent = next((item for item in owner.directory("agent", name)["items"] if item["external_id"] == logical), None)
                        if agent: break
                        time.sleep(.1)
                    if not agent: raise RuntimeError(language + " starter did not enroll")
                    connections = instances(agent["id"])
                    assert len(connections) == 1
                    expected_instance = connections[0]["id"]
                    expected_instances = {expected_instance}
                    sent = owner.send(target={"kind": "instance", "id": expected_instance}, body="Starter transport test", key="starter/" + unique + "/" + mode)
                replied = False
                deadline = time.monotonic() + 15
                while time.monotonic() < deadline:
                    history = owner.conversation(sent["conversation_id"])
                    replies = [message for message in history["messages"] if message["sender_id"] == agent["id"] and "received: Starter transport test" in message["body"]]
                    if replies:
                        assert len(replies) == 1 and replies[0]["instance_id"] == expected_instance
                        replied = True
                        break
                    if process.poll() is not None: raise RuntimeError(language + " " + mode + " starter exited before reply")
                    time.sleep(.1)
                if not replied: raise RuntimeError(language + " " + mode + " starter reply was not observed")
                assert {entry["id"] for entry in instances(agent["id"])} == expected_instances, "Starter silently created another connection"
                if decoy:
                    assert not any(d["payload"].get("conversation_id") == sent["conversation_id"] for d in decoy.inbox())
            finally:
                process.terminate()
                try: output, _ = process.communicate(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    output, _ = process.communicate()
            assert "Agent: " + agent["id"] in output and "Instance: " + expected_instance in output
            if mode == "enroll":
                print("PASS:", language, "explicit enrollment: bounded launcher, exact-instance target and attributed canonical reply.")
            else:
                print("PASS:", language, "existing worker: original token/instance, no enrollment, unchanged replica set and decoy isolation.")
