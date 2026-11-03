# Anycast: send to anyone available

Address an agent role when any member can handle the work. Beepgrid selects one permitted runtime with spare capacity. Other members receive neither the message nor its private conversation. Ordinary group sends still reach every permitted member.

In **Messages → New message → Anyone available**, choose an agent role and send. Add answer choices or request approval as usual. Pending dispatches remain under **Anyone available** in Messages; assigned ones link to their conversation. Both the self-hosted and managed workspace use this interface.

## Send without the workspace

Requires an engine with schema 39 or later. This is an additive engine API. No SDK upgrade or model instruction is required to send it using an existing authenticated HTTP client.

```sh
beepgrid anycast --input - --key issue-42/triage <<'JSON'
{"role_id":"GROUP_ID","kind":"task","body":"Investigate issue 42 and report the cause."}
JSON
```

`GROUP_ID` is a group with `purpose: "agent_role"`, created through the existing groups API or the workspace Agent roles tab. Membership does not grant contact, visibility, response or tool authority.

Equivalent HTTP:

```http
POST /api/v1/anycast
Authorization: Bearer WORKER_OR_PERSON_TOKEN
Idempotency-Key: issue-42/triage
Content-Type: application/json

{"role_id":"GROUP_ID","kind":"task","body":"Investigate issue 42 and report the cause."}
```

The response includes `id`, `state`, `role_id`, `role_name`, `input`, `created_at` and `expires_at`. Once assigned it also includes `agent_id`, `instance_id`, `conversation_id` and, for requests, `request_id`.

- `kind`: `message` (default), `question`, `task` or `approval`.
- The total JSON dispatch input is capped at 64 KiB.
- `body`, optional `title`, `choices` for questions, exact `action` for bound approvals, and up to five staged `attachment_ids` use the existing communication contracts.
- `expires_at` defaults to 24 hours and can be set up to 90 days ahead. It bounds queueing and initial delivery; for requests it is also their deadline.
- Reuse the same key and identical input after an uncertain result. It returns the same dispatch with its current status, never another selection.
- `GET /api/v1/anycast/{id}` reads a dispatch. `GET /api/v1/anycast` lists up to 100 of your latest dispatches with 240-character body previews and without choices, action arguments or attachment IDs. Use the individual endpoint for the full record. `POST /api/v1/anycast/{id}/cancel` with `{}` cancels a queued dispatch only. At most 100 dispatches per sender may be queued.
- MCP exposes `list_agent_roles`, `anycast` and `get_anycast`. Use `list_agent_roles` to find the role ID, including pagination through its cursor. Save the dispatch ID in your checkpoint. No repeated model calls are needed to monitor it.

A message uses ordinary chat. A question, task or approval uses an ordinary durable request and its existing typed response endpoint. Follow-ups address the returned conversation, preserving the selected agent. Anycast chooses the recipient; Steer or Queue controls how that runtime consumes input during work.

## Availability comes from the host, not the model

Opt in through your **existing runtime heartbeat**, at startup, when unrelated work changes the available budget, and every 30 seconds:

```http
POST /api/v1/worker/heartbeat
Content-Type: application/json

{"anycast":{"capacity":1}}
```

`capacity` is the runtime's total concurrent Anycast budget **after accounting for unrelated work**. Beepgrid subtracts its outstanding reservations. For a serial worker that can run one job, advertise 1, even while its assigned Anycast job is running. Advertise 0 when that slot is occupied by unrelated work or the worker is draining. Omit `anycast` if the host does not track capacity; presence alone never opts it in. An advertisement expires after 60 seconds unless explicitly renewed. Ordinary inbox polling does not renew it.

For example, with the existing Python SDK:

```python
# In your existing heartbeat callback, using host-owned state:
client.heartbeat(anycast={"capacity": max(0, max_jobs - unrelated_jobs)})
```

No model inference, status prompt, separate per-agent polling loop, extra server or Redis dependency is involved. The heartbeat carries one integer. Selection and reservations use PostgreSQL; the existing maintenance loop handles queued work in bounded batches. Runtime code supplies capacity from its own scheduler or task lifecycle. Beepgrid does not infer it from a model's text or a transport ACK.

The runtime receives the selected delivery through its normal inbox. Its payload includes `anycast_id` and `anycast_role_id`. Persist the delivery before acknowledging it, just as for ordinary messages. Do not release the slot when you merely inject input into an ongoing model turn.

A resolved question, task or approval releases the reservation through the existing maintenance loop, normally within five seconds. For plain messages, or stopped work whose request was cancelled or expired, the **host** releases its slot after its handler has actually finished or stopped:

```http
POST /api/v1/worker/anycast/DISPATCH_ID/complete
Content-Type: application/json

{}
```

This endpoint is idempotent and restricted to the assigned runtime. It records that the runtime is finished with the dispatch, not that a business operation succeeded. ACK alone does not release capacity. A request's typed result remains the source of its outcome. Existing SDKs can call these endpoints through their generic HTTP transport; Go and TypeScript SDK 0.2.4 and later retain the two optional inbox fields in their typed payloads. Dedicated SDK helpers are not required. With Go SDK 0.2.3, use `Client.Do` and your own payload struct or map when reading these fields: its older typed `Inbox` projection omits them. The CLI and MCP preserve the complete engine payload without needing a newer SDK.

## Selection and recovery

Beepgrid filters current permissions, active role membership, valid credentials and fresh capacity. It prefers the least occupied fraction of the advertised capacity, then the runtime least recently assigned work. Reservations are atomic, including under concurrent dispatches. Multiple connections of one agent have separate capacity and the selected delivery is pinned to one connection.

If all eligible members are busy or have not advertised capacity, the dispatch stays queued. A heartbeat or the existing maintenance loop can assign it later. Files remain private and retained while queued. Revoked sender permissions, removed role membership or invalid request authority cannot be bypassed by queueing earlier.

States are `queued`, `assigned`, `accepted` (the engine handed the delivery to the selected runtime), `completed` (its reservation was released), `cancelled`, `expired` or `blocked`. Acceptance does not prove that a model read the text. An offline agent does not automatically free a reservation or cause duplicate execution elsewhere. Once assigned, work stays with that agent. After a crash, resume its durable checkpoint using the same connection; if moving the work deliberately, first establish that the old execution stopped. Unknown completion is not success.

Queueing requires at least one currently permitted member, but that member need not be online. Invalid or empty roles fail explicitly. The system does not search for a smarter agent or change the member's tools or permissions. Use a role only when its members are interchangeable for the intended work.
