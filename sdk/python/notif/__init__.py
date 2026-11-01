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


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def _open(request, *, timeout):
    # A bearer credential must never follow a redirect to another origin.
    return urllib.request.build_opener(_NoRedirect()).open(request, timeout=timeout)


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
                with _open(req, timeout=self.timeout if timeout is None else timeout) as response:
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

    def events(self, **options: Any):
        """Yield canonical events. Save sequence only after applying each event."""
        from .events import read_events
        return read_events(self, **options)

    def start_chat(self, *, targets: list[dict[str, str]], body: str, title: str = "", approval: bool = False,
                   choices: list[str] | None = None, mentions: list[dict[str, Any]] | None = None,
                   attachment_ids: list[str] | None = None,
                   key: str | None = None) -> dict[str, Any]:
        """Start shared history. Mention offsets are code points; all members receive it."""
        data: dict[str, Any] = {"targets": targets, "body": body}
        if title: data["title"] = title
        if approval: data["approval"] = True
        if choices is not None: data["choices"] = choices
        if mentions is not None: data["mentions"] = mentions
        if attachment_ids is not None: data["attachment_ids"] = attachment_ids
        return self.call("POST", "/api/v1/conversations", data, key=key or str(uuid.uuid4()))

    def change_chat_membership(self, conversation_id: str, *, operation: str, version: int, title: str = "",
                               members: list[str] | None = None, share_history: bool = False,
                               next_manager: str = "", key: str | None = None) -> dict[str, Any]:
        """Change shared disclosure, never IAM. Earlier history is shared only by explicit opt-in."""
        data: dict[str, Any] = {"operation": operation, "version": version}
        if title: data["title"] = title
        if members is not None: data["members"] = members
        if share_history: data["share_history"] = True
        if next_manager: data["next_manager"] = next_manager
        return self.call("POST", "/api/v1/conversations/" + urllib.parse.quote(conversation_id, safe="") + "/membership", data, key=key or str(uuid.uuid4()))

    def attachments(self, conversation_id: str) -> list[dict[str, Any]]:
        return self.call("GET", "/api/v1/conversations/" + urllib.parse.quote(conversation_id, safe="") + "/attachments")

    def upload_attachment(self, conversation_id: str, name: str, media: str, data: bytes) -> dict[str, Any]:
        """No automatic retry: list attachments after an unknown upload outcome."""
        return self._upload_attachment("/api/v1/conversations/" + urllib.parse.quote(conversation_id, safe="") + "/attachments", name, media, data)

    def stage_attachment(self, name: str, media: str, data: bytes) -> dict[str, Any]:
        """Private draft for attachment_ids. No automatic retry; unused drafts expire after 24 hours."""
        return self._upload_attachment("/api/v1/attachments", name, media, data)

    def _upload_attachment(self, path: str, name: str, media: str, data: bytes) -> dict[str, Any]:
        if not data or len(data) > 5 * 1024 * 1024 or any(ch in name + media for ch in "\r\n"):
            raise ValueError("Invalid attachment or size exceeds 5 MiB")
        boundary = "beepgrid-" + uuid.uuid4().hex
        escaped = name.replace("\\", "\\\\").replace('"', '\\"')
        header = ('--' + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="' + escaped + '"\r\nContent-Type: ' + media + '\r\n\r\n').encode()
        raw = header + data + ("\r\n--" + boundary + "--\r\n").encode()
        return json.loads(self._file_request("POST", path, raw, "multipart/form-data; boundary=" + boundary))

    def download_attachment(self, attachment_id: str) -> bytes:
        return self._file_request("GET", "/api/v1/attachments/" + urllib.parse.quote(attachment_id, safe=""))

    def _file_request(self, method: str, path: str, data=None, content_type="") -> bytes:
        headers = {"Authorization": "Bearer " + self._token, "User-Agent": "NotifPython/0.1"}
        if content_type: headers["Content-Type"] = content_type
        req = urllib.request.Request(self.base_url + path, data=data, headers=headers, method=method)
        try:
            with _open(req, timeout=self.timeout) as response:
                raw = response.read(5 * 1024 * 1024 + 1)
                if len(raw) > 5 * 1024 * 1024: raise ValueError("Attachment response exceeds 5 MiB")
                return raw
        except urllib.error.HTTPError as error:
            with error:
                try: detail = json.loads(error.read(65536)).get("error", {})
                except (ValueError, AttributeError): detail = {}
            raise NotifError(detail.get("code", "HTTP_ERROR"), detail.get("message", "Attachment request rejected"), error.code) from error

    def directory(self, kind: str, search: str = "") -> dict[str, Any]:
        if kind not in ("agent", "person", "service"): raise ValueError("Invalid identity kind")
        return self.query("people" if kind == "person" else kind + "s", search=search)

    def conversation(self, conversation_id: str) -> dict[str, Any]:
        return self.call("GET", "/api/v1/conversations/" + urllib.parse.quote(conversation_id, safe=""))

    def provider_catalog(self) -> list[dict[str, Any]]:
        return self.call("GET", "/api/v1/integrations/catalog")

    def connection_setup(self, connection_id: str) -> dict[str, Any]:
        return self.call("GET", "/api/v1/connections/" + urllib.parse.quote(connection_id, safe="") + "/setup")

    def replace_request(self, request_id: str, version: int, body: str, *, key: str | None = None, mentions: list[dict[str, Any]] | None = None) -> dict[str, Any]:
        """Replace an own pending request. Retain key when retrying an unknown result."""
        payload = {"version": version, "body": body}
        if mentions is not None:
            payload["mentions"] = mentions
        return self.call("POST", "/api/v1/requests/" + urllib.parse.quote(request_id, safe="") + "/replace", payload, key=key or str(uuid.uuid4()))

    def cancel(self, request_id: str, version: int) -> dict[str, Any]:
        return self.call("POST", "/api/v1/requests/" + urllib.parse.quote(request_id, safe="") + "/cancel", {"version": version})

    def query(self, collection: str, **options: Any) -> dict[str, Any]:
        return self.call("POST", "/api/v1/query", {"collection": collection, **options})

    def capabilities(self, **options: Any) -> dict[str, Any]:
        return self.call("POST", "/api/v1/capabilities", options)

    def preview_contact_permissions(self, subject: dict[str, str], *, rules: list[dict[str, str]] | None = None,
                                    version: int = 0, targets: list[dict[str, str]] | None = None) -> dict[str, Any]:
        data: dict[str, Any] = {"subject": subject, "version": version, "targets": targets or []}
        if rules is not None:
            data["rules"] = rules
        return self.call("POST", "/api/v1/contact-permissions/preview", data)

    def save_contact_permissions(self, subject: dict[str, str], rules: list[dict[str, str]], *, version: int) -> dict[str, Any]:
        return self.call("POST", "/api/v1/contact-permissions", {"subject": subject, "rules": rules, "version": version})

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
             title: str = "", key: str | None = None, minimal: bool = False, preview_token: str = "", mentions: list[dict[str, Any]] | None = None, reuse_direct: bool = False, attachment_ids: list[str] | None = None) -> dict[str, Any]:
        data: dict[str, Any] = {"body": body, "conversation_id": conversation_id, "title": title}
        if mentions:
            data["mentions"] = mentions
        if reuse_direct:
            data["reuse_direct"] = True
        if attachment_ids is not None:
            data["attachment_ids"] = attachment_ids
        if target:
            data["target"] = target
        if preview_token:
            data["preview_token"] = preview_token
        return self.call("POST", "/api/v1/messages", data, key=key or str(uuid.uuid4()), minimal=minimal)

    def notify(self, **options: Any) -> dict[str, Any]:
        """Durable acceptance only. send with the same input/key recovers the full receipt."""
        return self.send(**{**options, "minimal": True})

    def ask(self, *, target: dict[str, str], title: str = "", key: str | None = None, **options: Any) -> dict[str, Any]:
        return self.call("POST", "/api/v1/requests", {"target": target, "title": title, **options}, key=key or str(uuid.uuid4()))

    def set_request_blocking(self, request_id: str, blocking: bool) -> dict[str, Any]:
        return self.call("POST", "/api/v1/requests/" + urllib.parse.quote(request_id, safe="") + "/blocking", {"blocking": blocking})

    def coordinate(self, prompt: str, *, key: str | None = None, **options: Any) -> dict[str, Any]:
        return self.call("POST", "/api/v1/fleet/coordinate", {"prompt": prompt, **options}, key=key or str(uuid.uuid4()), timeout=max(40, self.timeout))

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


def mention_context(message: dict[str, Any], recipient: str) -> str:
    """An addressing hint. Other room participants may still contribute."""
    mentions = message.get("mentions", [])
    if not mentions:
        return ""
    if any(m.get("principal_id") == recipient for m in mentions):
        return "You are explicitly addressed. Other participants may contribute relevant context."
    return "Other participants are explicitly addressed. You may contribute relevant context."
