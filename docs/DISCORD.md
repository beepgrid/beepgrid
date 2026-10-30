# Discord for a self-hosted OSS workspace

The adapter uses an operator-owned Discord application and bot. No Slack Enterprise, Discord enterprise plan, BeepGrid managed account or paid message provider is required by this adapter. Discord's normal account, installation and platform rules still apply.

This first release supports private bot DMs through signed HTTP interactions. Use `/beepgrid message` to start a conversation, the **Reply** button to submit a reply form, and **Approve** or **Deny** for versioned approvals. Ordinary DM text, guild messages, Gateway listeners and native file transfer are unsupported. Files remain in the authenticated BeepGrid workspace.

## Setup

1. Create a dedicated application in the [Discord Developer Portal](https://discord.com/developers/applications). Keep privileged intents disabled. Save the immutable application ID, public key and the bot token. Use a bot token, never your personal Discord account token.
2. Save an enabled `discord` connection with `config: {application_id, public_key, default_agent_id?}` and encrypted secret JSON `{"bot_token":"..."}`. Public keys are hexadecimal Ed25519 keys. Store the bot credential server-side.
3. Read `GET /api/v1/connections/{id}/setup`. Set its `callbacks.interactions` as the application's HTTPS Interactions Endpoint URL. A valid signed PING must succeed; invalid signatures must fail. No dashboard or owner API needs public exposure.
4. The setup's optional `registration` contains the exact global command descriptor, command endpoint and installation URL. Register this single command on the dedicated application. The API does not register commands or modify an existing application for you. Install `bot` and `applications.commands` into an authorized server with zero guild permission bits. This initial adapter accepts only the bot DM context.
5. Have the intended Person initiate the bot interaction. An authorized identity administrator links immutable IDs with `POST /api/v1/people/{id}/discord-account`, body `{application_id, user_id}`. Add a destination with that exact linked user ID. Mutable names and arbitrary channel IDs are rejected. Linking does not grant communication or decision rights; grant the specific test contact authority separately.
6. Queue `POST /api/v1/connections/{id}/test` with `destination_id` and a stable `Idempotency-Key`. Verify receipt in the selected DM, use **Reply**, submit the form and inspect canonical Person attribution. Opening a form alone does not qualify the connection. Test a synthetic approval separately.

`registration` contains no credentials. Its command descriptor restricts contexts to the bot DM and integration type to guild installation. Application distribution, additional installation contexts and user-installed app support are separate work. No Administrator, Message Content, Presence or Member intent is requested.

## Authority and recovery

Ingress verifies `X-Signature-Timestamp` and `X-Signature-Ed25519` against the exact body, with bounded timestamp skew. The application ID, current connection version, enabled state, private context, linked Person and mapped source message are checked again before a canonical mutation. Native approvals use the current request version and current decision authority. Message/decision and dedup receipt commit together before acknowledgment; a bounded database deadline avoids intentionally acknowledging work before it is durable.

Replaying the same interaction ID is idempotent. Reusing an ID with changed canonical input conflicts. Opening a reply form has no `conversation_id` qualification receipt. Submitted replies and decisions can supply correlated evidence. Saving the connection invalidates test evidence.

Unlinking/relinking a Person stops old callbacks and queued destinations. Disabling the connection blocks callbacks and queued sends. A different Discord application requires a new connection. Revoke/uninstall the dedicated application separately in Discord; local disconnect does not claim provider revocation.

Discord delivery opens or reuses the linked user's private DM, verifies its single recipient and sends bounded text with mentions disabled. Delivery remains subject to the user's DM access and Discord's restrictions. Rate-limit responses carry a bounded retry delay. A lost response or server error has an unknown outcome requiring reconciliation. A stable `nonce` plus `enforce_nonce` reduces duplicates within Discord's limited deduplication window; it is not indefinite provider idempotency.

## Verification boundary

PostgreSQL and signed HTTP fixtures cover concurrent replay, changed payloads, Person attribution, private context isolation, stale/current decisions, relinking, disconnect, diagnostic reset, rate limits and unknown outcomes. Fixture success is not a live Discord account exchange. Keep live qualification evidence separate.

Migration 033 introduces the Discord kind and identity uniqueness indexes after the committed file/history migrations 031 and 032. Apply the complete migration chain before serving SchemaVersion 33. Normal migration-path PostgreSQL tests cover that composition.

Official references: [first bot setup](https://docs.discord.com/developers/quick-start/getting-started), [HTTP interaction preparation](https://docs.discord.com/developers/interactions/overview), [responses and initial deadline](https://docs.discord.com/developers/interactions/receiving-and-responding), [alternatives to privileged intents](https://docs.discord.com/developers/gateway/you-might-not-need-a-privileged-intent), [private DM creation](https://docs.discord.com/developers/resources/user#create-dm), [message bounds and nonce behavior](https://docs.discord.com/developers/resources/message#create-message).
