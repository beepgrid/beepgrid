-- Shared chat disclosure uses commit-ordered event watermarks, never clocks.
ALTER TABLE conversations ADD COLUMN shared boolean NOT NULL DEFAULT false;
ALTER TABLE conversations ADD COLUMN version integer NOT NULL DEFAULT 1;
ALTER TABLE conversations ADD COLUMN manager_id text NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN preview_sequence bigint NOT NULL DEFAULT 0;
UPDATE conversations c SET shared=true,manager_id=created_by WHERE (SELECT count(*) FROM participants p WHERE p.scope=c.scope AND p.conversation_id=c.id)>2;
ALTER TABLE participants ADD COLUMN history_start bigint NOT NULL DEFAULT 0;
ALTER TABLE participants ADD COLUMN left_at timestamptz;
ALTER TABLE messages ADD COLUMN created_sequence bigint NOT NULL DEFAULT 0;
ALTER TABLE requests ADD COLUMN created_sequence bigint NOT NULL DEFAULT 0;
ALTER TABLE attachments ADD COLUMN created_sequence bigint NOT NULL DEFAULT 0;
UPDATE messages m SET created_sequence=e.sequence FROM (SELECT scope,data->>'id' id,min(sequence) sequence FROM events WHERE kind='message.created' GROUP BY scope,data->>'id') e WHERE m.scope=e.scope AND m.id=e.id;
UPDATE requests r SET created_sequence=e.sequence FROM (SELECT scope,data->>'id' id,min(sequence) sequence FROM events WHERE kind='request.created' GROUP BY scope,data->>'id') e WHERE r.scope=e.scope AND r.id=e.id;
UPDATE attachments a SET created_sequence=e.sequence FROM (SELECT scope,data->>'id' id,min(sequence) sequence FROM events WHERE kind='attachment.created' GROUP BY scope,data->>'id') e WHERE a.scope=e.scope AND a.id=e.id;
CREATE FUNCTION chat_history_allows(s text,c text,p text,admin boolean,origin bigint) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT admin OR EXISTS(SELECT 1 FROM participants WHERE scope=s AND conversation_id=c AND principal_id=p AND left_at IS NULL AND history_start<=origin)
$$;
CREATE FUNCTION record_chat_origin() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='message.created' THEN UPDATE messages SET created_sequence=NEW.sequence WHERE scope=NEW.scope AND id=NEW.data->>'id' AND created_sequence=0;
 ELSIF NEW.kind='request.created' THEN UPDATE requests SET created_sequence=NEW.sequence WHERE scope=NEW.scope AND id=NEW.data->>'id' AND created_sequence=0;
 ELSIF NEW.kind='attachment.created' THEN UPDATE attachments SET created_sequence=NEW.sequence WHERE scope=NEW.scope AND id=NEW.data->>'id' AND created_sequence=0;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER chat_origin AFTER INSERT ON events FOR EACH ROW EXECUTE FUNCTION record_chat_origin();
CREATE OR REPLACE FUNCTION update_conversation_activity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE origin bigint;
BEGIN
 origin:=NEW.sequence;
 IF NEW.kind='request.resolved' THEN SELECT created_sequence INTO origin FROM requests WHERE scope=NEW.scope AND id=NEW.data->>'id'; END IF;
 UPDATE conversations SET last_activity_at=GREATEST(last_activity_at,NEW.created_at),preview_sequence=COALESCE(origin,0),
 last_preview=left(CASE WHEN NEW.kind='request.resolved' THEN COALESCE(NEW.data->'response'->>'outcome','Response received') ELSE COALESCE(NULLIF(NEW.data->>'body',''),NEW.data->>'title','') END,240)
 WHERE scope=NEW.scope AND id=NEW.conversation_id AND last_activity_at<=NEW.created_at;
 RETURN NEW;
END $$;
UPDATE conversations c SET preview_sequence=e.origin FROM (SELECT DISTINCT ON(e.scope,e.conversation_id) e.scope,e.conversation_id,CASE WHEN e.kind='request.resolved' THEN COALESCE(r.created_sequence,0) ELSE e.sequence END origin FROM events e LEFT JOIN requests r ON r.scope=e.scope AND r.id=e.data->>'id' WHERE e.kind IN ('message.created','request.created','request.resolved') ORDER BY e.scope,e.conversation_id,e.sequence DESC) e WHERE c.scope=e.scope AND c.id=e.conversation_id;
CREATE INDEX participants_active_member ON participants(scope,principal_id,conversation_id) WHERE left_at IS NULL;

CREATE FUNCTION chat_delivery_allows(s text,c text,op text,p text,admin boolean) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE((SELECT NOT cv.shared OR chat_history_allows(s,c,p,admin,COALESCE((SELECT created_sequence FROM messages WHERE scope=s AND id=op),(SELECT created_sequence FROM requests WHERE scope=s AND id=split_part(op,'/',1)),0)) FROM conversations cv WHERE cv.scope=s AND cv.id=c),c='')
$$;
