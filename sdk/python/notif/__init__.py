"""Notif's dependency-free synchronous application and worker client."""
from __future__ import annotations
import json
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
            raise ValueError("Invalid Notif server URL")
        self.base_url, self._token, self.timeout = base_url.rstrip("/"), token, timeout

    def call(self, method: str, path: str, data: Any = None, *, key: str | None = None) -> Any:
        if not path.startswith("/api/v1/"):
            raise ValueError("Invalid API path")
        raw = json.dumps(data).encode() if data is not None else None
        headers = {"Content-Type": "application/json", "Authorization": "Bearer " + self._token}
        if key:
            headers["Idempotency-Key"] = key
        for attempt in range(3):
            req = urllib.request.Request(self.base_url + path, raw, headers, method=method)
            try:
                with urllib.request.urlopen(req, timeout=self.timeout) as response:
                    return json.load(response)
            except urllib.error.HTTPError as error:
                try:
                    detail = json.load(error).get("error", {})
                except (ValueError, AttributeError):
                    detail = {}
                raise NotifError(detail.get("code", "HTTP_ERROR"), detail.get("message", "Request failed"), error.code) from error
            except (urllib.error.URLError, TimeoutError):
                if attempt == 2 or (method != "GET" and not key):
                    raise
                time.sleep(.2 * (attempt + 1))
        raise RuntimeError("Request attempts exhausted")

    def connect(self, agent: str, name: str, capabilities: list[str] | None = None,
                metadata: dict[str, Any] | None = None) -> tuple[Client, dict[str, Any]]:
        session = self.call("POST", "/api/v1/sessions", {"agent": agent, "name": name,
                            "capabilities": capabilities or [], "metadata": metadata or {}})
        return Client(self.base_url, session["token"], self.timeout), session

    def send(self, *, body: str, target: dict[str, str] | None = None, conversation_id: str = "",
             title: str = "", key: str | None = None) -> dict[str, Any]:
        data: dict[str, Any] = {"body": body, "conversation_id": conversation_id, "title": title}
        if target:
            data["target"] = target
        return self.call("POST", "/api/v1/messages", data, key=key or str(uuid.uuid4()))

    def ask(self, *, target: dict[str, str], title: str, key: str | None = None, **options: Any) -> dict[str, Any]:
        return self.call("POST", "/api/v1/requests", {"target": target, "title": title, **options}, key=key or str(uuid.uuid4()))

    def get_request(self, request_id: str) -> dict[str, Any]:
        return self.call("GET", "/api/v1/requests/" + urllib.parse.quote(request_id, safe=""))

    def respond(self, request_id: str, outcome: str, *, version: int, answer: Any = None) -> dict[str, Any]:
        return self.call("POST", "/api/v1/requests/" + urllib.parse.quote(request_id, safe="") + "/responses",
                         {"outcome": outcome, "version": version, "answer": answer})

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
            except (urllib.error.URLError, TimeoutError):
                time.sleep(2)
                continue
            for delivery in deliveries:
                handler(delivery)
                self.ack(delivery)
            time.sleep(1)


__all__ = ["Client", "NotifError"]
