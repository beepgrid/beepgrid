# Channel delivery preferences

Agents address a Person. Beepgrid chooses the delivery channels and keeps one canonical conversation. No Agent prompt, runtime listener or extra acknowledgement is needed.

## Configure in the workspace

Open **Integrations → Delivery preferences**. Drag channels into order, or focus a drag handle and use the arrow keys. Choose **Preferred channel, then fallbacks** or **All selected channels**. Addresses are managed under **Delivery destinations**.

- **Preferred:** start with the first enabled, permitted destination. A definite delivery failure tries the next one immediately, after any safe provider retries.
- **All selected:** send to every selected destination immediately. This is opt-in.
- **Only in Beepgrid:** remove every destination from the list. Authorized conversation history stays in the workspace.
- **Unanswered requests:** optionally choose a delay before trying the next destination. This applies only to open questions and approvals, not ordinary messages or task completion. The delay starts when the provider accepts the previous notification. Each subsequent channel gets the same delay.

Provider acceptance does not prove that a Person read a notification. Unknown or ambiguous provider outcomes pause that delivery chain for reconciliation; they do not trigger automatic duplicate sends, including timed follow-ups.

Answering through any supported channel or the workspace resolves the same request and stops pending follow-ups. Cancelling, replacing or expiring the request also prevents later delivery. A network send already in flight cannot be recalled.

## Agent and Agent role overrides

Use **Preferences for** to choose an Agent or Agent role. Turn off **Use default** and customize the same channel stack. Turn it back on to remove that override.

Resolution is deterministic:

1. An individual Agent override.
2. The first matching Agent role override, in the displayed role priority order.
3. The Person's default policy.
4. Existing destination modes if no default has been saved.

Agent roles use current `agent_role` group memberships at send time, not IAM permission roles. Drag the role priority stack when more than one role override exists. Overrides replace a policy rather than merging channel lists.

Verified incoming provider threads establish reply affinity. For a configured policy, the originating installation must still be selected. Its current thread replaces that installation's ordinary destination for the reply; outbound receipts never add more notification copies. A workspace-only policy never routes back to an old provider thread.

Preferences select among existing destinations and grant no contact, disclosure, decision or provider permissions. Beepgrid rechecks current credentials, recipient access, destination revision, enabled connection and applicable identity links before dispatch.

Saving changed preferences stops pending external sends from the previous configuration, including scheduled follow-ups. New sends use the new configuration. History is retained. Existing Primary, Fallback and Copy settings continue until a default or matching override replaces them.

## Configure without the UI

Normal and headless builds share this functionality. A Person can edit their own policy; administrators and explicitly authorized identity managers can configure other People. Obtain destination IDs from `GET /api/v1/destinations`, Agent IDs from the directory, and Agent role IDs from groups with `purpose: "agent_role"`.

Read the current version with `GET /api/v1/people/{person_id}/delivery-preferences`. Save defaults and overrides atomically:

```http
POST /api/v1/people/{person_id}/delivery-preferences
Content-Type: application/json

{
  "version": 0,
  "default": {
    "mode": "preferred",
    "destinations": ["dst_slack", "dst_telegram", "dst_email"],
    "followup_seconds": 900
  },
  "overrides": [
    {
      "source": {"kind": "agent", "id": "agt_reviewer"},
      "policy": {
        "mode": "all",
        "destinations": ["dst_slack", "dst_email"],
        "followup_seconds": 0
      }
    },
    {
      "source": {"kind": "group", "id": "grp_builders"},
      "policy": {
        "mode": "preferred",
        "destinations": ["dst_telegram", "dst_email"],
        "followup_seconds": 1800
      }
    }
  ]
}
```

Use the returned version for the next write. A stale version returns `409`; reload instead of overwriting another edit. `default: null` preserves legacy defaults. An empty `destinations` list means workspace-only delivery. Omit an override to restore inheritance. The first matching group entry wins, regardless of where individual Agent entries appear.

Limits: 16 destinations per policy and 64 overrides per Person. Follow-up delay is `0` (off), or 60 to 604800 seconds. All-channel policies require `0`. Missing or disabled channels are skipped. Native history remains when native contact is permitted.

TypeScript:

```ts
const preferences = await client.deliveryPreferences(personId);
preferences.default = {
  mode: "preferred",
  destinations: [slackDestinationId, emailDestinationId],
  followup_seconds: 900,
};
await client.saveDeliveryPreferences(personId, preferences);
```

Python: `client.delivery_preferences(person_id)` and `client.save_delivery_preferences(person_id, preferences)`.

Go: `client.DeliveryPreferences(ctx, personID)` and `client.SaveDeliveryPreferences(ctx, personID, preferences)`.

No additional server, broker, scheduler deployment or model call is required. The existing PostgreSQL delivery worker stores route order, retry state and due times durably. Only configured follow-ups add provider sends. Dispatch previews include selected routes and are invalidated when preferences change.

Requires schema migration 035. Tests use isolated PostgreSQL and local provider fixtures, not live-provider qualification.
