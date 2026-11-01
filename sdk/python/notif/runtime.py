"""Durable, independent input for existing async Agent loops. No model dependency.

Use one connection exclusively. Tool-only MCP should use --live-input so it
cannot compete for this inbox. Local custody is not model/business completion.
"""

from __future__ import annotations
import asyncio
import json
import os
from pathlib import Path
import sqlite3
import time
import uuid
import contextvars
import urllib.error
from typing import Any, Callable, Awaitable


class LiveInbox:
    def __init__(
        self,
        client,
        path: str,
        identity: str,
        *,
        mode: str = "steer",
        max_pending: int = 1000,
    ):
        if (
            mode not in ("steer", "queue")
            or not identity
            or path == ":memory:"
            or max_pending < 1
        ):
            raise ValueError(
                "Use steer or queue, an exact connection identity and a persistent path"
            )
        self.client, self.mode, self.max_pending = client, mode, max_pending
        Path(path).parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.db = sqlite3.connect(path)
        os.chmod(path, 0o600)
        self.db.execute("PRAGMA journal_mode=WAL")
        self.db.execute("PRAGMA synchronous=FULL")
        self.db.executescript("""
          CREATE TABLE IF NOT EXISTS identity(id INTEGER PRIMARY KEY CHECK(id=1),value TEXT NOT NULL);
          CREATE TABLE IF NOT EXISTS lease(id INTEGER PRIMARY KEY CHECK(id=1),owner TEXT NOT NULL,expires INTEGER NOT NULL);
          CREATE TABLE IF NOT EXISTS inputs(id TEXT PRIMARY KEY,payload TEXT NOT NULL,state TEXT NOT NULL,created INTEGER NOT NULL);
        """)
        self.owner = str(uuid.uuid4())
        try:
            with self.db:
                self.db.execute("BEGIN IMMEDIATE")
                row = self.db.execute(
                    "SELECT value FROM identity WHERE id=1"
                ).fetchone()
                if row and row[0] != identity:
                    raise ValueError("Inbox belongs to another connection")
                self.db.execute(
                    "INSERT OR IGNORE INTO identity VALUES(1,?)", (identity,)
                )
                row = self.db.execute("SELECT expires FROM lease WHERE id=1").fetchone()
                if row and row[0] > self.now():
                    raise ValueError("This inbox already has a receiver")
                self.db.execute(
                    "INSERT OR REPLACE INTO lease VALUES(1,?,?)",
                    (self.owner, self.now() + 30000),
                )
        except BaseException:
            self.db.close()
            raise
        self.changed = asyncio.Event()
        self.tasks: set[asyncio.Task] = set()
        self.receiving = None
        self.renewing = None
        self.ready = False

    @staticmethod
    def now():
        return int(time.time() * 1000)

    def guard(self):
        row = self.db.execute("SELECT owner,expires FROM lease WHERE id=1").fetchone()
        if not row or row[0] != self.owner or row[1] <= self.now():
            raise RuntimeError("Receiver lease lost; restart before receiving")

    def pending(self, conversation: str | None = None):
        self.guard()
        rows = self.db.execute(
            "SELECT payload FROM inputs WHERE state!='accepted' ORDER BY created,rowid"
        ).fetchall()
        values = [json.loads(row[0]) for row in rows]
        return [
            v
            for v in values
            if conversation is None or v["conversationId"] == conversation
        ]

    def put(self, delivery):
        self.guard()
        payload = delivery["payload"]
        conversation = (
            payload.get("conversation_id")
            or payload.get("message", {}).get("conversation_id")
            or payload.get("request", {}).get("conversation_id")
        )
        if not conversation or not delivery.get("id"):
            raise ValueError("Missing delivery identity")
        with self.db:
            if self.db.execute(
                "SELECT 1 FROM inputs WHERE id=?", (delivery["id"],)
            ).fetchone():
                return
            if (
                self.db.execute(
                    "SELECT count(*) FROM inputs WHERE state!='accepted'"
                ).fetchone()[0]
                >= self.max_pending
            ):
                raise RuntimeError("Local inbox full; receiving paused")
            value = dict(
                id=delivery["id"],
                conversationId=conversation,
                kind=payload["type"],
                delivery=delivery,
            )
            self.db.execute(
                "INSERT INTO inputs VALUES(?,?,'pending',?)",
                (delivery["id"], json.dumps(value), self.now()),
            )
            self.db.execute(
                "DELETE FROM inputs WHERE state='accepted' AND created<?",
                (self.now() - 31 * 86400000,),
            )
        self.changed.set()

    def checkpoint(self, ids):
        """Only after host state containing these IDs is durably checkpointed."""
        self.guard()
        with self.db:
            self.db.executemany(
                "UPDATE inputs SET state='accepted',payload=json_remove(payload,'$.delivery.payload.message','$.delivery.payload.request') WHERE id=?",
                [(i,) for i in ids],
            )

    async def receive(self):
        while True:
            self.guard()
            with self.db:
                self.db.execute(
                    "UPDATE lease SET expires=? WHERE owner=?",
                    (self.now() + 30000, self.owner),
                )
            try:
                jobs = await asyncio.to_thread(
                    self.client.call, "GET", "/api/v1/worker/inbox?wait_ms=15000"
                )
                self.ready = True
                for job in jobs:
                    self.put(job)
                    await asyncio.to_thread(self.client.ack, job)
                if not jobs:
                    await asyncio.sleep(0.05)
            except asyncio.CancelledError:
                raise
            except (urllib.error.URLError, TimeoutError, ConnectionError):
                await asyncio.sleep(0.5)
            except Exception as error:
                status = getattr(error, "status", None)
                if status in (409, 429) or status and status >= 500:
                    await asyncio.sleep(0.5)
                elif str(error).startswith("Local inbox full"):
                    await asyncio.sleep(0.5)
                else:
                    raise

    async def renew(self):
        while True:
            await asyncio.sleep(5)
            self.guard()
            with self.db:
                self.db.execute(
                    "UPDATE lease SET expires=? WHERE owner=?",
                    (self.now() + 30000, self.owner),
                )

    def start(self):
        if self.receiving:
            raise RuntimeError("Already started")
        self.receiving = asyncio.create_task(self.receive())
        self.renewing = asyncio.create_task(self.renew())
        return self

    async def close(self):
        if self.receiving:
            self.receiving.cancel()
            await asyncio.gather(self.receiving, return_exceptions=True)
        if self.renewing:
            self.renewing.cancel()
            await asyncio.gather(self.renewing, return_exceptions=True)
        for task in self.tasks:
            task.cancel()
        await asyncio.gather(*self.tasks, return_exceptions=True)
        with self.db:
            self.db.execute("DELETE FROM lease WHERE owner=?", (self.owner,))
        self.db.close()


def input_text(value):
    """Keep external input, sender, mentions, attachments and correlation as data."""
    return json.dumps(
        {
            "beepgrid": {
                **value["delivery"]["payload"],
                "delivery_id": value["id"],
                "conversation_id": value["conversationId"],
            }
        }
    )


class LangGraphBridge:
    """LangChain Agent middleware and LangGraph checkpoint-compatible receiving.

    Configure the graph with a durable checkpointer and before_model(). Existing
    tools/model/session state remain owned by the application. serve() owns one
    invocation per conversation, checks the final checkpoint and catches the
    input-arrived-at-completion race. It never updates a running graph externally.
    """

    def __init__(self, inbox: LiveInbox, *, thread_id: Callable[[str], str] | None = None):
        self.inbox = inbox
        self.thread_id = thread_id or (lambda conversation: conversation)
        self.active: dict[str, asyncio.Task] = {}
        self.initial: dict[str, set[str]] = {}
        self.conversation = contextvars.ContextVar("beepgrid_conversation")

    def before_model(self):
        from langchain.agents.middleware import before_model
        from langchain_core.messages import HumanMessage

        @before_model
        async def receive_input(state, runtime):
            conversation = self.conversation.get()
            existing = {getattr(m, "id", None) for m in state.get("messages", [])}
            inputs = self.inbox.pending(conversation)
            if self.inbox.mode == "queue":
                inputs = [
                    i
                    for i in inputs
                    if i["id"] in self.initial.get(conversation, set())
                    or i["kind"] == "request.resolved"
                ]
            messages = [
                HumanMessage(content=input_text(i), id="beepgrid/" + i["id"])
                for i in inputs
                if "beepgrid/" + i["id"] not in existing
            ]
            return {"messages": messages} if messages else None

        return receive_input

    async def serve(
        self, graph, *, on_result: Callable[[str, Any, str], Awaitable[None]]
    ):
        if not graph.checkpointer:
            raise ValueError("Configure the existing graph with a durable checkpointer")
        if not self.inbox.receiving:
            self.inbox.start()

        async def publish(conversation, values, persisted):
            ids = sorted(
                i["id"]
                for i in self.inbox.pending(conversation)
                if "beepgrid/" + i["id"] in persisted
            )
            if ids:
                import hashlib

                key = (
                    "beepgrid-result/"
                    + hashlib.sha256(json.dumps(ids).encode()).hexdigest()
                )
                await on_result(conversation, values, key)
                self.inbox.checkpoint(ids)

        async def run(conversation):
            self.conversation.set(conversation)
            config = {"configurable": {"thread_id": self.thread_id(conversation)}}
            try:
                while self.inbox.pending(conversation):
                    snapshot = await graph.aget_state(config)
                    if snapshot.next:
                        raise RuntimeError(
                            "Graph is paused; resume its native interrupt/approval instead of starting another run"
                        )
                    existing = {
                        getattr(m, "id", None)
                        for m in snapshot.values.get("messages", [])
                    }
                    await publish(conversation, snapshot.values, existing)
                    values = self.inbox.pending(conversation)
                    if not values:
                        break
                    self.initial[conversation] = {i["id"] for i in values}
                    # Queue includes only the batch at invocation start. Steer also
                    # receives subsequent arrivals through the local middleware.
                    from langchain_core.messages import HumanMessage

                    result = await graph.ainvoke(
                        {
                            "messages": [
                                HumanMessage(
                                    content=input_text(i), id="beepgrid/" + i["id"]
                                )
                                for i in values
                            ]
                        },
                        config,
                    )
                    snapshot = await graph.aget_state(config)
                    if snapshot.next:
                        raise RuntimeError(
                            "Graph paused; leave custody pending until native resume"
                        )
                    persisted = {
                        getattr(m, "id", None)
                        for m in snapshot.values.get("messages", [])
                    }
                    if not any("beepgrid/" + i["id"] in persisted for i in values):
                        raise RuntimeError("Host checkpoint did not retain input IDs")
                    await publish(conversation, result, persisted)
            finally:
                self.active.pop(conversation, None)
                self.initial.pop(conversation, None)
                self.inbox.changed.set()

        while True:
            if self.inbox.receiving.done():
                await self.inbox.receiving
            if self.inbox.renewing.done():
                await self.inbox.renewing
            if self.inbox.ready:
                for conversation in dict.fromkeys(
                    i["conversationId"] for i in self.inbox.pending()
                ):
                    if conversation not in self.active and len(self.active) < 8:
                        task = asyncio.create_task(run(conversation))
                        self.active[conversation] = task
                        self.inbox.tasks.add(task)
            self.inbox.changed.clear()
            completed = [t for t in self.inbox.tasks if t.done()]
            for task in completed:
                self.inbox.tasks.remove(task)
                await task  # Do not silently retry failed business jobs.
            try:
                await asyncio.wait_for(self.inbox.changed.wait(), 0.25)
            except asyncio.TimeoutError:
                pass
