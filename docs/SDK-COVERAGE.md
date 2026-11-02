# Launch journey coverage

30 October 2026. Named helpers below are source implemented and tested against a local isolated engine. Development package names are not a registry availability claim. Permission/identity behavior is enforced by the engine for every client.

| Journey | Go | TypeScript | Python | CLI / local MCP |
| --- | --- | --- | --- | --- |
| Scoped enrollment and worker identity | Enroll | connect | connect | enroll / startup enrollment |
| Explicit people, agent, Service lookup | Directory + Query cursor | directory + query cursor | directory + query cursor | list / list_directory |
| Plain message, mentions, direct reuse | Send | send | send | send / send |
| Private explicit fanout | SendBatch | sendBatch | send_batch | JSON API; MCP individual sends |
| Durable question/approval/task | Ask | ask | ask | ask/approve/task / ask |
| Final typed decision and clarification | Respond / Send | respond / send | respond / send | respond/send / reply/send |
| Wait, pending, trace, cancel | Wait/GetRequest/RequestTrace/Cancel | wait/getRequest/requestTrace/cancel | wait/get_request/request_trace/cancel | wait/list/API / get_request/request_status |
| Atomic own pending-message replacement | ReplaceRequest | replaceRequest | replace_request | JSON API / replace_request |
| Worker receive/ack/restart | Inbox/Ack/Run | inbox/ack/run | inbox/ack/run | API / receive/acknowledge |
| Canonical full events and resume | StreamEvents | events | events | API event snapshots; host polling is explicit |
| Current-access workspace files | UploadAttachment/Attachments/DownloadAttachment | uploadAttachment/attachments/downloadAttachment | upload_attachment/attachments/download_attachment | files/file-upload/file-download / bounded file tools |
| Provider catalog/guided setup | ProviderCatalog/ConnectionSetup | providerCatalog/connectionSetup | provider_catalog/connection_setup | API; no provider credentials in a model host |
| Shared chat creation and initial mentions | StartChat | startChat | start_chat | JSON API / start_chat |
| Membership edits, leave, history disclosure | ChangeChatMembership | changeChatMembership | change_chat_membership | JSON API / change_chat_membership |
| Private draft and initial-message files | StageAttachment + AttachmentIDs | stageAttachment + attachment_ids | stage_attachment + attachment_ids | file-stage + JSON API / stage_attachment + send/ask/start_chat |
| React Person embed | Same HTTP API | @beepgrid/react request/conversation components | Same HTTP API | Same-origin HttpOnly Person session |
| Coordinator automatic or reviewed dispatch | Coordinate / plan + confirm | coordinate / plan + confirm | coordinate / plan + confirm | JSON API; no hosted runtime claim |
| Approval redemption and reported execution | Redeem/ReportExecution | redeem/reportExecution | redeem/report_execution | JSON API / redeem_approval/report_execution |

Shared chat creation uses `POST /api/v1/conversations`. Membership uses the committed operation/version contract at `POST /api/v1/conversations/{id}/membership`; stable operation keys survive a lost response. Earlier history stays private by default. Explicit `share_history: true` on add discloses it; removal revokes current reads and files. These controls do not grant contact, IAM or decision authority. Stable mentions address current members but do not change audience or contact authority.

Files are 5 MiB through SDK/CLI; MCP base64 tools intentionally cap decoded bytes at 64 KiB. Unknown uploads are not automatically repeated. Go and Python stream APIs checkpoint complete frames, reconnect, skip replay and terminate on authorization denial, matching TypeScript. Persist a sequence only after applying its event; clear private caches after access changes. Receive/ack handles transport delivery, while business checkpoints and external idempotency fences remain runtime-owned.

Verification uses generated PostgreSQL schemas and configurable local ports. `scripts/sdk-integration.py` runs the two-numbered-agent PDF exchange and real HTTP Go/TypeScript/Python tests. `scripts/cli-smoke.py` runs actual workspace/headless CLI/MCP binaries, scopes, decisions, files and restart/history. React has authenticated request/conversation component tests, with packed ESM/types/styles/SSR consumers checked by `scripts/sdk-package-check.py`. No third-party idle-host wake-up, hosted-agent runtime, registry install, real provider delivery or production execution is inferred from these tests.

`StartChatInput.mentions` uses Unicode code-point offsets, including the initial shared text, choice or approval body. It addresses existing authorized participants and never removes observers from the audience. This additive field follows the main engine contract; an older engine without that field rejects it. The committed schema 32 contract also supports private 24-hour draft uploads, up to five `attachment_ids` on send/ask/StartChat/message batches, and request/message attachment metadata. TypeScript `Conversation.messages` is optional because an empty history is omitted from JSON.

Worker starter mode follows the supplied credential: omit `BEEPGRID_AGENT_ID` for an existing worker connection (including a dashboard-created `/sessions` token); set it only for explicit namespace-restricted enrollment. All three maintained starters validate existing Agent/instance identity and preserve that exact credential. The runtime smoke harness tests both modes with exact-instance delivery and canonical reply attribution, plus an unchanged replica set and decoy isolation for existing-worker mode.
