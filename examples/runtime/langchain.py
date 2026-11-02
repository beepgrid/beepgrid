"""Add Beepgrid input to an existing LangChain Agent. Run from the source checkout.

Use your existing model, tools and production checkpointer below. This module
exports a helper rather than selecting a provider or installing a new runtime.
"""

import os
from pathlib import Path
from beepgrid import Client
from beepgrid.runtime import LiveInbox, LangGraphBridge


def connect():
    client = Client(
        os.environ["BEEPGRID_URL"],
        Path(os.environ["BEEPGRID_TOKEN_FILE"]).read_text().strip(),
    )
    inbox = LiveInbox(
        client,
        os.environ["BEEPGRID_INBOX_PATH"],
        os.environ["BEEPGRID_CONNECTION_ID"],
        mode=os.environ.get("BEEPGRID_BUSY_MODE", "steer"),
    )
    return client, inbox, LangGraphBridge(inbox)


# In your application:
# client, inbox, bridge = connect()
# agent = create_agent(your_model, tools=your_tools,
#                      middleware=[*your_middleware, bridge.before_model()],
#                      checkpointer=your_durable_checkpointer)
# async def publish(conversation, result, key):
#     await asyncio.to_thread(client.send, conversation_id=conversation,
#                             body=result['messages'][-1].content, key=key)
# try:
#     await bridge.serve(agent, on_result=publish)
# finally:
#     await inbox.close()
