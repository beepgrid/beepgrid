#!/usr/bin/env python3
"""Local product-review workspace. Uses real APIs, never production data.

Start a --dev engine with company='Beepgrid Lab', environment='review', and a
dedicated NOTIF_SCHEMA. Then run seed, followed by workers. Credentials are
written only to the ignored, private state directory. Responses are deliberate
fixtures, not model execution or provider qualification.
"""
import argparse
import concurrent.futures
import hashlib
import http.server
import json
import os
from pathlib import Path
import signal
import threading
import time
import urllib.error
import urllib.parse
import urllib.request


class Review:
    def __init__(self, args):
        url = urllib.parse.urlsplit(args.url)
        if url.scheme != "http" or url.hostname not in ("127.0.0.1", "localhost", "::1") or url.username or url.password or url.path not in ("", "/") or url.query or url.fragment:
            raise SystemExit("Review fixtures require a plain loopback HTTP origin.")
        self.base = args.url.rstrip("/") + "/api/v1"
        self.root = args.state.resolve()
        self.root.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.root.chmod(0o700)
        self.file = self.root / "fixture.json"
        self.state = json.loads(self.file.read_text()) if self.file.exists() else {"agents": {}, "people": {}, "steps": {}}
        self.owner = args.owner_token.read_text().strip()
        self.me = self.call("/me")
        if self.me.get("company") != "Beepgrid Lab" or self.me.get("environment") != "review" or not self.me.get("admin"):
            raise SystemExit("Refusing to populate anything except the isolated Beepgrid Lab / review administrator workspace.")
        fingerprint = hashlib.sha256(self.owner.encode()).hexdigest()
        if self.state.get("workspace") not in (None, fingerprint):
            raise SystemExit("This fixture state belongs to a different workspace. Use a separate state directory.")
        self.state["workspace"] = fingerprint
        self.state["owner_id"] = self.me["person"]["id"]

    def save(self):
        tmp = self.file.with_suffix(".tmp")
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as out:
            json.dump(self.state, out)
        os.replace(tmp, self.file)

    def call(self, path, data=None, token=None, key=None):
        headers = {"Authorization": "Bearer " + (token or self.owner), "Content-Type": "application/json"}
        if key:
            headers["Idempotency-Key"] = "review-v1-" + key
        req = urllib.request.Request(self.base + path, data=json.dumps(data).encode() if data is not None else None, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=25) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            # The API's error does not contain credentials. Never print inputs.
            problem = json.loads(error.read())
            raise RuntimeError(f"{path}: {error.code}: {problem}") from None

    def once(self, label, path, data, token=None):
        if label not in self.state["steps"]:
            self.state["steps"][label] = self.call(path, data, token, label)
            self.save()
        return self.state["steps"][label]

    def seed(self):
        existing = self.call("/people")
        for name, department in [("Maya Chen", "Engineering"), ("Sam Patel", "Support")]:
            if name not in self.state["people"]:
                person = next((p for p in existing if p["external_id"] == "review/" + name), None)
                person = person or self.call("/people", {"name": name, "external_id": "review/" + name, "department": department, "email": name.split()[0].lower() + "@example.test"})
                credential = self.call("/credentials", {"principal_id": person["id"], "kind": "api"})
                self.state["people"][name] = {"person": person, **credential}
                self.save()
        descriptions = {
            "Builder": "API clients and checkout services",
            "Reviewer": "Code review and release checks",
            "DevOps": "Deployments and staging infrastructure",
            "Billing": "Payment reconciliation and invoice records",
        }
        for role, description in descriptions.items():
            for number in range(1, 4):
                name = f"{role} {number}"
                if name not in self.state["agents"]:
                    self.state["agents"][name] = self.call("/sessions", {"agent": "review/" + name.lower().replace(" ", "-"), "name": name, "capabilities": ["message", "status"], "metadata": {"description": description, "runtime": "Python" if number == 2 else "Go", "fixture": True}})
                    self.save()
        agents = self.state["agents"]
        people = self.state["people"]
        contacts = [self.me["person"], *[p["person"] for p in people.values()], *[a["agent"] for a in agents.values()]]
        roles, bindings = self.call("/roles"), self.call("/role-bindings")
        for subject in contacts[1:]:
            # Sam is intentionally restricted, so review can detect accidental
            # disclosure. All other identities may see their collaboration peers.
            peers = contacts if subject["name"] != "Sam Patel" else [self.me["person"], agents["Builder 1"]["agent"]]
            version = next((r["version"] for r in roles if r["id"].startswith("rol_contacts_") and any(b["role_id"] == r["id"] and b["subject_id"] == subject["id"] for b in bindings)), 0)
            self.call("/contact-permissions", {"subject": {"kind": subject["kind"], "id": subject["id"]}, "version": version, "rules": [{"action": action, "target_kind": peer["kind"], "target_id": peer["id"]} for peer in peers if peer["id"] != subject["id"] for action in ("view", "message", "ask", "decide")]})
        groups = self.call("/groups")
        role_names = {"Builder": "Builders", "Reviewer": "Reviewers", "DevOps": "DevOps", "Billing": "Billing"}
        group_specs = [(role_names[role], "agent_role", [a["agent"]["id"] for name, a in agents.items() if name.startswith(role)]) for role in descriptions]
        group_specs += [("Engineering", "team", [self.me["person"]["id"], people["Maya Chen"]["person"]["id"]]), ("Support", "team", [people["Sam Patel"]["person"]["id"]])]
        for name, purpose, members in group_specs:
            legacy = {"DevOps": "DevOpss", "Billing": "Billings"}.get(name, name)
            old = next((g for g in groups if g["name"] in (name, legacy) and g["purpose"] == purpose), None)
            self.call("/groups", {**({"id": old["id"], "version": old["version"]} if old else {}), "name": name, "purpose": purpose, "members": members})
        self.once("builder-replica", "/sessions", {"agent": agents["Builder 1"]["agent"]["external_id"], "name": "Builder 1", "capabilities": ["message", "status"]})
        target = lambda name: {"kind": "agent", "id": agents[name]["agent"]["id"]}
        chat = self.once("shared-rollout", "/conversations", {"title": "V2 checkout rollout", "targets": [target("Builder 1"), target("Reviewer 1"), target("DevOps 2")], "body": "Can we migrate the checkout workers to the V2 endpoint today? Keep staging available for the 3 pm demo."})
        cid = chat["conversation_id"]
        for label, name, body in [
            ("builder-update", "Builder 1", "@DevOps 2 I’m migrating the checkout jobs to /v2/payments. Can you keep concurrency at eight while they move?"),
            ("devops-update", "DevOps 2", "Concurrency is capped at eight. I’ll leave the demo worker pool on V1 until the migration is confirmed."),
            ("reviewer-update", "Reviewer 1", "Retries preserve the original idempotency key. The V1 fallback stays enabled for the demo pool."),
        ]:
            mentions = []
            if body.startswith("@DevOps 2"):
                mentions = [{"principal_id": target("DevOps 2")["id"], "start": 0, "end": len("@DevOps 2")}]
            self.once(label, "/messages", {"conversation_id": cid, "body": body, "mentions": mentions}, agents[name]["token"])
        for i, (name, question, reply) in enumerate([
            ("Builder 2", "Is the webhook retry fix ready for review?", "The retry fix is on branch checkout/v2-retries. Duplicate callbacks return the original result, and the rollback path stays intact."),
            ("DevOps 1", "Can we reserve staging for the demo?", "Yes. The demo pool stays on V1, so the checkout migration can continue without interrupting the demo."),
            ("Reviewer 2", "Which tests still need a pass?", "The API contract suite passed. Billing 2 is checking settlement records before I close the reconciliation review."),
        ]):
            sent = self.once(f"direct-{i}", "/messages", {"target": target(name), "body": question, "reuse_direct": True})
            self.once(f"reply-{i}", "/messages", {"conversation_id": sent["conversation_id"], "body": reply}, agents[name]["token"])
        billing = self.once("ledger-question", "/messages", {"target": target("Billing 2"), "body": "Orders 1042 and 1048 have captured payments but still show pending after yesterday’s webhook retry. Can you check the ledger?", "reuse_direct": True}, agents["Builder 1"]["token"])
        self.upload_pdf(billing["conversation_id"], agents["Builder 1"]["token"])
        self.once("ledger-reply", "/messages", {"conversation_id": billing["conversation_id"], "body": "Both captures settled at 14:32 UTC. The retry repeated the webhook event ID, so these orders need a status refresh, not another charge."}, agents["Billing 2"]["token"])
        self.once("staging-approval", "/requests", {"target": {"kind": "person", "id": self.me["person"]["id"]}, "kind": "approval", "body": "Can I restart the staging checkout workers? The demo pool will stay online."}, agents["DevOps 2"]["token"])
        self.once("maya-approval", "/requests", {"target": {"kind": "person", "id": people["Maya Chen"]["person"]["id"]}, "kind": "approval", "body": "The old settlement index is no longer queried. Can I remove it after today’s demo?"}, agents["Billing 1"]["token"])
        for i, (sender, recipient, body) in enumerate([
            ("Builder 3", "Reviewer 3", "Which commit contains the signed V2 schema? I need it to finish the client."),
            ("Reviewer 3", "DevOps 3", "Can you provide the deployed schema hash? I need it to finish the review."),
            ("DevOps 3", "Builder 3", "Can you publish the V2 client build? I need it to generate the deployment schema hash."),
        ]):
            self.once(f"cycle-{i}", "/requests", {"target": target(recipient), "kind": "question", "blocking": True, "body": body}, agents[sender]["token"])
        for name in ("Builder 1", "Reviewer 1", "Billing 2"):
            self.once("shared-question-" + name, "/requests", {"target": {"kind": "person", "id": self.me["person"]["id"]}, "kind": "question", "body": "What is the checkout API freeze time for the V2 rollout?", "shared_key": "review/v2-api-freeze"}, agents[name]["token"])
        self.save()
        self.inspect()

    def upload_pdf(self, conversation, token):
        if "report" in self.state["steps"]:
            return
        boundary = "beepgrid-review-file"
        pdf = b"%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Count 0/Kids[]>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF"
        body = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"checkout-orders.pdf\"\r\nContent-Type: application/pdf\r\n\r\n").encode() + pdf + f"\r\n--{boundary}--\r\n".encode()
        req = urllib.request.Request(self.base + f"/conversations/{conversation}/attachments", data=body, headers={"Authorization": "Bearer " + token, "Content-Type": "multipart/form-data; boundary=" + boundary})
        with urllib.request.urlopen(req) as response:
            self.state["steps"]["report"] = json.load(response)
        self.save()

    def inspect(self):
        fleet = self.call("/fleet/status", {})
        print(json.dumps({"workspace": self.me["company"], "environment": self.me["environment"], "agents": len(fleet["agents"]), "connected": sum(a["fresh"] for a in fleet["agents"]), "roles_and_teams": len(self.call("/groups")), "conversations": len(self.call("/conversations")), "blocking_cycles": len(fleet.get("deadlocks", [])), "fixture_steps": len(self.state["steps"]), "external_providers": "not configured", "response_mode": "deterministic local worker"}, indent=2))

    def workers(self):
        stop = threading.Event()
        for sig in (signal.SIGINT, signal.SIGTERM):
            signal.signal(sig, lambda *_: stop.set())
        people = {self.state["owner_id"], *[p["person"]["id"] for p in self.state["people"].values()]}
        seeded = {m["id"] for result in self.state["steps"].values() if isinstance(result, dict) for m in result.get("messages", [])}

        def run(name, session):
            token = session["token"]
            while not stop.is_set():
                try:
                    self.call("/worker/heartbeat", {}, token)
                    for delivery in self.call("/worker/inbox?wait_ms=2000", token=token):
                        payload = delivery["payload"]
                        message = payload.get("message", {})
                        if payload.get("type") == "message" and message.get("sender_id") in people and message.get("id") not in seeded:
                            body = message.get("body", "").lower()
                            reply = "The demo pool stays available. I’ll use the separate checkout worker pool and keep concurrency at eight." if "staging" in body or "demo" in body else "The V2 migration keeps the original idempotency keys and the V1 fallback. I can check the next change in this thread."
                            self.call("/messages", {"conversation_id": message["conversation_id"], "body": reply}, token, "worker-" + delivery["id"])
                        # Receipt is separate from a completed request. In
                        # particular, never auto-approve or resolve the cycle.
                        self.call(f"/worker/inbox/{delivery['id']}/ack", {"generation": delivery["generation"]}, token)
                except (OSError, RuntimeError, ValueError):
                    if not stop.is_set():
                        print(f"{name}: connection unavailable; retrying", flush=True)
                        stop.wait(3)
                stop.wait(0.25)
        live = [(name, a) for name, a in self.state["agents"].items() if name != "Billing 3"]
        live.append(("Builder 1 / replica", self.state["steps"]["builder-replica"]))
        print("Local fixture workers running. Billing 3 stays offline; blocking questions and approvals remain pending. Ctrl-C stops only these workers.", flush=True)
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(live)) as pool:
            jobs = [pool.submit(run, name, session) for name, session in live]
            for job in jobs:
                job.result()

    def open(self):
        """One-shot loopback sign-in, keeping fixture secrets out of URLs/chat."""
        token = self.owner if args.as_person == "owner" else self.state["people"][args.as_person]["token"]
        req = urllib.request.Request(self.base + "/auth/login", data=json.dumps({"token": token}).encode(), headers={"Content-Type": "application/json", "Origin": "http://127.0.0.1:4040"})
        with urllib.request.urlopen(req) as response:
            cookie = response.headers["Set-Cookie"]
        class Login(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path != "/open":
                    self.send_error(404)
                    return
                self.send_response(303)
                self.send_header("Set-Cookie", cookie)
                self.send_header("Location", "http://127.0.0.1:4040/agents")
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
            def log_message(self, *_):
                pass
        with http.server.HTTPServer(("127.0.0.1", 4011), Login) as server:
            server.timeout = 60
            print("Open http://127.0.0.1:4011/open once to review as " + args.as_person + ". No token is placed in the URL.", flush=True)
            server.handle_request()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["seed", "workers", "inspect", "open"])
    parser.add_argument("--as-person", choices=["owner", "Maya Chen", "Sam Patel"], default="owner")
    parser.add_argument("--url", default="http://127.0.0.1:4010")
    parser.add_argument("--state", type=Path, default=Path(".local/product-review"))
    parser.add_argument("--owner-token", type=Path, default=Path(".local/product-review/.local/owner-token"))
    args = parser.parse_args()
    review = Review(args)
    getattr(review, args.command)()
