import asyncio, tempfile
from langchain.agents import create_agent
from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.outputs import ChatResult, ChatGeneration
from langchain_core.messages import AIMessage
from langchain_core.tools import tool
from langgraph.checkpoint.memory import InMemorySaver
from notif.runtime import LiveInbox, LangGraphBridge
from pydantic import PrivateAttr


class Model(BaseChatModel):
    _calls: list = PrivateAttr(default_factory=list)

    @property
    def _llm_type(self):
        return "beepgrid-contract-model"

    def bind_tools(self, tools, **kw):
        return self

    def _generate(self, *a, **kw):
        raise NotImplementedError

    async def _agenerate(self, messages, **kw):
        self._calls.append(messages)
        if len(self._calls) == 1:
            return ChatResult(
                generations=[
                    ChatGeneration(
                        message=AIMessage(
                            content="",
                            tool_calls=[
                                dict(
                                    name="work", args={}, id="work-1", type="tool_call"
                                )
                            ],
                        )
                    )
                ]
            )
        return ChatResult(
            generations=[
                ChatGeneration(
                    message=AIMessage(
                        content="Kept V1"
                        if any("Keep V1" in str(m.content) for m in messages)
                        else "Task done"
                    )
                )
            ]
        )


async def case(mode):
    started = asyncio.Event()
    release = asyncio.Event()
    published = []

    @tool
    async def work() -> str:
        """Read the next existing work segment."""
        started.set()
        await release.wait()
        return "segment complete"

    class Client:
        def call(self, *a):
            return []

        def ack(self, *a):
            pass

    with tempfile.TemporaryDirectory() as folder:
        inbox = LiveInbox(
            Client(), folder + "/inbox.db", "workspace/exact-instance", mode=mode
        )
        bridge = LangGraphBridge(inbox, thread_id=lambda c: "existing/" + c)
        model = Model()
        graph = create_agent(
            model,
            tools=[work],
            middleware=[bridge.before_model()],
            checkpointer=InMemorySaver(),
        )

        async def result(c, value, key):
            published.append((c, value, key))

        serve = asyncio.create_task(bridge.serve(graph, on_result=result))

        def put(id, text):
            inbox.put(
                {
                    "id": id,
                    "generation": 1,
                    "payload": {
                        "type": "message",
                        "conversation_id": "con_a",
                        "message": {"sender_id": "person_alice", "body": text},
                    },
                }
            )

        put("first", "Review migration")
        await asyncio.wait_for(started.wait(), 3)
        put("correction", "Keep V1")
        release.set()
        for i in range(100):
            if not inbox.pending():
                break
            if serve.done():
                await serve
            await asyncio.sleep(0.03)
        assert not inbox.pending(), inbox.pending()
        snapshot = await graph.aget_state({"configurable": {"thread_id": "existing/con_a"}})
        assert snapshot.values.get("messages"), "Existing thread mapping was not preserved"
        second = repr([m.content for m in model._calls[1]])
        assert ("Keep V1" in second) == (mode == "steer"), (mode, second)
        assert len(model._calls) == (2 if mode == "steer" else 3)
        assert len(published) == (1 if mode == "steer" else 2)
        assert all(key.startswith("beepgrid-result/") for _, _, key in published)
        serve.cancel()
        await asyncio.gather(serve, return_exceptions=True)
        await inbox.close()
    print(
        "PASS",
        mode,
        "one model loop, boundary injection, checkpoint IDs, result publication",
    )


asyncio.run(case("steer"))
asyncio.run(case("queue"))
