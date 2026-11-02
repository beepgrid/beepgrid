-- Draft files remain private to their uploader until a communication commits.
ALTER TABLE attachments ALTER COLUMN conversation_id DROP NOT NULL;
ALTER TABLE attachments ADD COLUMN draft_until timestamptz;
ALTER TABLE attachments ADD COLUMN bound_operation text NOT NULL DEFAULT '';
ALTER TABLE attachments ADD CONSTRAINT attachments_scope_id UNIQUE(scope,id);
CREATE TABLE attachment_links (
 scope text NOT NULL, attachment_id text NOT NULL, conversation_id text NOT NULL,
 message_id text, request_id text,
 CHECK ((message_id IS NOT NULL)::integer + (request_id IS NOT NULL)::integer = 1),
 FOREIGN KEY(scope,attachment_id) REFERENCES attachments(scope,id) ON DELETE CASCADE,
 FOREIGN KEY(scope,conversation_id) REFERENCES conversations(scope,id),
 FOREIGN KEY(message_id) REFERENCES messages(id) ON DELETE CASCADE,
 FOREIGN KEY(scope,request_id) REFERENCES requests(scope,id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX attachment_message_unique ON attachment_links(scope,attachment_id,message_id) WHERE message_id IS NOT NULL;
CREATE UNIQUE INDEX attachment_request_unique ON attachment_links(scope,attachment_id,request_id,conversation_id) WHERE request_id IS NOT NULL;
CREATE INDEX attachment_links_conversation ON attachment_links(scope,conversation_id);
CREATE INDEX attachment_links_message ON attachment_links(scope,message_id) WHERE message_id IS NOT NULL;
CREATE INDEX attachment_links_request ON attachment_links(scope,request_id) WHERE request_id IS NOT NULL;
