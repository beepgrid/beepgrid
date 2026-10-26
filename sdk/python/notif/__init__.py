"""Beep Grid's dependency-free synchronous application and worker client."""
from __future__ import annotations
import json
import http.client
import time
import uuid
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Callable


class NotifError(Exception):
    def __init__(self, code: str, message: str, status: int):
        super().__init__(f"{code}: {message}")
        self.code, self.message, self.status = code, message, status


class Client:
    def __init__(self, base_url: str, token: str, timeout: float = 20):
        parsed = urllib.parse.urlsplit(base_url)
        if parsed.scheme not in ("http", "https") or not parsed.netloc or parsed.username:
            raise ValueError("Invalid Beep Grid server URL")
        self.base_url, self._token, self.timeout = base_url.rstrip("/"), token, timeout

    def call(self, method: str, path: str, data: Any = None, *, key: str | None = None, minimal: bool = False, timeout: float | None = None) -> Any:
        if not path.startswith("/api/v1/"):
            raise ValueError("Invalid API path")
        raw = json.dumps(data).encode() if data is not None else None
        headers = {"Content-Type": "application/json", "Authorization": "Bearer " + self._token,
                   "User-Agent": "NotifPython/0.1"}
        if minimal:
            headers["Prefer"] = "return=minimal"
        if key:
            headers["Idempotency-Key"] = key
        for attempt in range(3):
            req = urllib.request.Request(self.base_url + path, raw, headers, method=method)
            try:
                with urllib.request.urlopen(req, timeout=self.timeout if timeout is None else timeout) as response:
                    return json.load(response)
            except urllib.error.HTTPError as error:
                try:
                    with error:
                        detail = json.load(error).get("error", {})
                except (ValueError, AttributeError):
                    detail = {}
                raise NotifError(detail.get("code", "HTTP_ERROR"), detail.get("message", "Request failed"), error.code) from error
            except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.IncompleteRead):
                if attempt == 2 or (method != "GET" and not key):
                    raise
                time.sleep(.2 * (attempt + 1))
        raise RuntimeError("Request attempts exhausted")

    def query(self, collection: str, **options: Any) -> dict[str, Any]:
        return self.call("POST", "/api/v1/query", {"collection": collection, **options})

    def capabilities(self, **options: Any) -> dict[str, Any]:
        return self.call("POST", "/api/v1/capabilities", options)

    def access(self, subject_id: str = "") -> dict[str, Any]:
        return self.call("GET", "/api/v1/access?subject_id=" + urllib.parse.quote(subject_id, safe=""))

    def dispatch_preview(self, targets: list[dict[str, str]], operation: str, **options: Any) -> dict[str, Any]:
        return self.call("POST", "/api/v1/dispatch-preview", {"targets": targets, "operation": operation, **options})

    def send_batch(self, targets: list[dict[str, str]], body: str, *, key: str | None = None, **options: Any) -> list[dict[str, Any]]:
        return self.call("POST", "/api/v1/message-batches", {"targets": targets, "body": body, **options}, key=key or str(uuid.uuid4()))

    def ask_batch(self, targets: list[dict[str, str]], request: dict[str, Any], *, key: str | None = None, **options: Any) -> list[dict[str, Any]]:
        return self.call("POST", "/api/v1/request-batches", {"targets": targets, "request": request, **options}, key=key or str(uuid.uuid4()))

    def request_trace(self, request_id: str, cursor: str = "") -> dict[str, Any]:
        return self.call("GET", "/api/v1/requests/" + urllib.parse.quote(request_id, safe="") + "/trace?delivery_cursor=" + urllib.parse.quote(cursor, safe=""))

    def workspace(self) -> dict[str, Any]:
        return self.call("GET", "/api/v1/workspace")

    def account(self) -> dict[str, Any]:
        return self.call("GET", "/api/v1/workspace/account")

    def connect(self, agent: str, name: str, capabilities: list[str] | None = None,
                metadata: dict[str, Any] | None = None) -> tuple[Client, dict[str, Any]]:
        session = self.call("POST", "/api/v1/sessions", {"agent": agent, "name": name,
                            "capabilities": capabilities or [], "metadata": metadata or {}})
        return Client(self.base_url, session["token"], self.timeout), session

    def send(self, *, body: str, target: dict[str, str] | None = None, conversation_id: str = "",
             title: str = "", key: str | None = None, minimal: bool = False, preview_token: str = "") -> dict[str, Any]:
        data: dict[str, Any] = {"body": body, "conversation_id": conversation_id, "title": title}
        if target:
            data["target"] = target
        if preview_token:
            data["preview_token"] = preview_token
        return self.call("POST", "/api/v1/messages", data, key=key or str(uuid.uuid4()), minimal=minimal)

    def notify(self, **options: Any) -> dict[str, Any]:
        """Durable acceptance only. send with the same input/key recovers the full receipt."""
        return self.send(**{**options, "minimal": True})

    def ask(self, *, target: dict[str, str], title: str, key: str | None = None, **options: Any) -> dict[str, Any]:
        return self.call("POST", "/api/v1/requests", {"target": target, "title": title, **options}, key=key or str(uuid.uuid4()))

    def get_request(self, request_id: str) -> dict[str, Any]:
        return self.call("GET", "/api/v1/requests/" + urllib.parse.quote(request_id, safe=""))

    def respond(self, request_id: str, outcome: str, *, version: int, answer: Any = None) -> dict[str, Any]:
        return self.call("POST", "/api/v1/requests/" + urllib.parse.quote(request_id, safe="") + "/responses",
                         {"outcome": outcome, "version": version, "answer": answer})

    def assistance(self) -> dict[str, Any]:
        return self.call("GET", "/api/v1/assistance/settings")

    def update_assistance(self, *, overseer: bool | None = None, version: int | None = None) -> dict[str, Any]:
        settings = {key: value for key, value in {"overseer": overseer, "version": version}.items() if value is not None}
        return self.call("POST", "/api/v1/assistance/settings", settings)

    def plan_overseer(self, prompt: str, target: dict[str, str] | None = None, *, previous_plan_id: str | None = None, key: str | None = None) -> dict[str, Any]:
        data: dict[str, Any] = {"prompt": prompt}
        if target is not None:
            data["target"] = target
        if previous_plan_id is not None:
            data["previous_plan_id"] = previous_plan_id
        options = {"key": key} if key is not None else {}
        return self.call("POST", "/api/v1/fleet/plan", data, timeout=max(40, self.timeout), **options)

    def overseer_plans(self) -> list[dict[str, Any]]:
        return self.call("GET", "/api/v1/fleet/plans")

    def overseer_plan(self, plan_id: str) -> dict[str, Any]:
        return self.call("GET", "/api/v1/fleet/plans/" + urllib.parse.quote(plan_id, safe=""))

    def confirm_overseer(self, plan_id: str) -> dict[str, Any]:
        return self.call("POST", "/api/v1/fleet/command", {"plan_id": plan_id})

    def summarize_overseer(self, plan_id: str) -> dict[str, Any]:
        return self.call("POST", "/api/v1/fleet/plans/" + urllib.parse.quote(plan_id, safe="") + "/summary",
                         {}, timeout=max(40, self.timeout))

    def redeem(self, request_id: str, action: dict[str, Any]) -> dict[str, Any]:
        """Recheck authorization; the business tool must enforce operation/version fences."""
        return self.call("POST", "/api/v1/requests/" + urllib.parse.quote(request_id, safe="") + "/redeem",
                         action, key=str(uuid.uuid4()))

    def report_execution(self, execution_id: str, state: str, result: Any = None) -> dict[str, Any]:
        """Report observed state without equating delivery or approval with execution."""
        return self.call("POST", "/api/v1/executions/" + urllib.parse.quote(execution_id, safe=""),
                         {"state": state, "result": result}, key=str(uuid.uuid4()))

    def wait(self, request_id: str, timeout: float | None = None, interval: float = 1) -> dict[str, Any]:
        deadline = None if timeout is None else time.monotonic() + timeout
        while True:
            if deadline is not None and time.monotonic() >= deadline:
                raise TimeoutError("Stopped waiting; the durable request remains active")
            request = self.get_request(request_id)
            if request["state"] != "open":
                return request
            remaining = interval if deadline is None else min(interval, max(0, deadline - time.monotonic()))
            time.sleep(remaining)

    def inbox(self) -> list[dict[str, Any]]:
        return self.call("GET", "/api/v1/worker/inbox")

    def ack(self, delivery: dict[str, Any]) -> None:
        self.call("POST", "/api/v1/worker/inbox/" + urllib.parse.quote(delivery["id"], safe="") + "/ack",
                  {"generation": delivery["generation"]})

    def heartbeat(self, **progress: Any) -> None:
        self.call("POST", "/api/v1/worker/heartbeat", progress)

    def run(self, handler: Callable[[dict[str, Any]], None], stop: Callable[[], bool] = lambda: False) -> None:
        while not stop():
            try:
                deliveries = self.call("GET", "/api/v1/worker/inbox?wait_ms=15000")
            except NotifError as error:
                if error.status < 500 and error.status != 429:
                    raise
                time.sleep(2)
                continue
            except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.IncompleteRead):
                time.sleep(2)
                continue
            for delivery in deliveries:
                handler(delivery)
                self.ack(delivery)
            if not deliveries:
                time.sleep(.1)


__all__ = ["Client", "NotifError"]
