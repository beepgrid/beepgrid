# Contact permissions

Contact access is directional: it describes what one Person, Agent or software service can do toward a contact. Teams and Agent roles can supply shared defaults. Chat membership controls disclosure separately and does not grant contact or external-tool authority.

| Permission | Allows |
| --- | --- |
| See | Discover the contact and read otherwise eligible conversation history |
| Message | Send ordinary messages to the contact |
| Request a reply | Ask a question, assign a task or request an approval |
| Approve / decline | Decide approval requests from that contact, subject to any action conditions |

In the workspace, enabling communication also enables See. Turning See off creates blocks for the dependent actions. Approvals are always an explicit choice. Permissions are checked in the engine, with no external authorization service.

## Defaults and exceptions

Rules omit `effect` for an allow, or explicitly use `allow`. An exact contact/group rule with `effect: "deny"` is a block across every route. A matching block wins over all direct, inherited and wildcard allows. Blocks are communication-only and cannot carry connection or action conditions. Administrator credentials retain full access; an ordinary policy is not an administrator restriction.

For example, if Builders may message Reviewers, an individual block for Builder toward Reviewer stops that contact, including group fan-out and existing shared chats. Removing and re-adding Builder to its Agent role does not clear the individual exception. **Use defaults** removes the dedicated contact policy's rules for that contact, restoring the remaining effective policies. If another policy blocks access, it must be changed at its source.

The editor shows effective access and its source. Unchecking inherited access creates a block. It does not merely remove one allow while leaving another active. A short review appears for new blocks or missing prerequisites, with the exact contact and actions affected. Concurrent edits require reloading before saving.

## Replies and approvals

If A may ask B and B can see A, B can answer that question or report the task's outcome without general Message permission toward A. This authority belongs to the specific request and original audience. It does not authorize another message, a different thread or extra recipients. Explicit Message blocks still stop that response. Visibility, the original Ask grant, group membership, request state and expiry are rechecked at response time.

Approval authority remains separate. A may ask B for approval only if at least one recipient can currently read and decide that request. Descriptor-specific conditions apply. Asking never makes B an approver. A new question, task or approval with no eligible responder returns `NO_ELIGIBLE_RESPONDER` and writes nothing.

Intentional one-way ordinary messaging remains supported. Changes do not invent reverse contact grants or modify other identities as a side effect.

## API and SDKs

`POST /api/v1/contact-permissions` saves a complete dedicated contact policy using `subject`, `rules` and the observed `version`. A block is an ordinary rule with `effect: "deny"`. Removing a rule means default, not block. Other policies remain assigned.

`POST /api/v1/contact-permissions/preview` accepts `subject` and up to 100 `targets`. Omit `rules` to observe current access. Include `rules` and `version` to preview an authorized draft. It returns the current direct policy, effective action cells with sources, warnings and save eligibility. It never saves or grants authority. `/permissions/explain` includes both `matched_grants` and `matched_blocks`.

Go, TypeScript and Python clients expose `PreviewContactPermissions` / `previewContactPermissions` / `preview_contact_permissions` and corresponding save methods. Pass an empty rule list to restore the dedicated policy's defaults; omit rules only when observing a preview.

## Compatibility and rollout

Existing rules remain allows. No schema migration or accepted-request rewrite is required. The response change deliberately enables intended recipients to answer existing questions/tasks where only general Message permission was missing. Before rollout, add explicit blocks where operators intended that absence to prohibit replies. Missing visibility, revoked Ask, audience removal and approval conditions continue to deny responses. Existing unusable requests stay durable; new unusable submissions are rejected.

Deploy compatible engine and workspace assets together. Older policy writers that do not preserve the `effect` field must be upgraded before editing policies containing blocks. No production deployment is performed by local qualification.
