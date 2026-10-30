# Shared chats and message files

Shared chats have one history visible to their current authorized members. Private multi-recipient sends still create separate conversations. An Agent role or a people Team is a directory group, not a chat.

## Member controls

Open a shared chat and select its member count. The chat manager or a workspace administrator can rename it, add members or remove them. Members can leave. A departing manager selects an existing member as the next manager while other members remain.

New members see messages from joining onward. **Share earlier history** explicitly includes earlier messages, decisions and files. Rejoining starts a new history window unless the manager deliberately shares earlier history. Existing workspace administrators retain their audit access.

Membership never creates contact permissions or business-tool authority. Everyone must already be allowed to view the other disclosed identities. Sending and responding still check current permissions. Sharing an earlier approval does not add a new member to its original decision audience. Historical identities remain subject to current View checks, including after someone leaves.

Removal or leaving stops subsequent reads, file downloads, event replay and pending delivery claims. Rejoining does not resurrect queued messages from the earlier membership. A removed member's open UI clears its cached chat when the access-change event arrives. Previously downloaded content cannot be recalled.

`POST /api/v1/conversations/{id}/membership` takes an `Idempotency-Key` and:

```json
{"operation":"add","version":3,"members":["agt_example"],"share_history":false}
```

Operations are `add`, `remove`, `rename` and `leave`. Use `title` for rename and `next_manager` when transferring management. Read the latest conversation version before editing. Replaying an accepted operation returns its original result; stale versions conflict. Shared chats support up to 100 members.

## Message files

Use **Attach files** in the composer, including for a new conversation. The file appears under the message or decision it belongs to. Files can be the entire message. Up to five files, each at most 5 MiB, are supported: text, JSON, PDF, PNG, JPEG and WebP.

The UI stages uploads privately with `POST /api/v1/attachments`. Only the uploader can read an unbound draft. Sending atomically claims `attachment_ids`, associates them with the accepted message or request and makes them available to authorized readers. Another send cannot reuse a claimed draft; a private batch can use the same object in each isolated copy within that operation. Abandoned drafts expire after 24 hours. Closing the composer discards its file selection; attach the file again when reopening it.

Downloads require authentication and current chat/history access. Upload acceptance is not message acceptance. Unknown uploads are not automatically retried. Existing conversation-level upload/list/download routes remain compatible; the current UI uses staged message files.

Schema migrations 031 and 032 add draft associations and commit-ordered membership windows. Workspace exports include message/file links and history windows. Apply migrations before running the new UI. Named SDK lifecycle/file-association wrappers are a separate integration handoff; the HTTP contract above is implemented.
