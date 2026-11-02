-- Conversation activity commits atomically with its canonical event. This is
-- presentation metadata, never a separate delivery or authority decision.
ALTER TABLE conversations ADD COLUMN last_activity_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE conversations ADD COLUMN last_preview text NOT NULL DEFAULT '';
UPDATE conversations SET last_activity_at=created_at;
UPDATE conversations c SET last_activity_at=e.created_at,last_preview=left(CASE WHEN e.kind='request.resolved' THEN COALESCE(e.data->'response'->>'outcome','Response received') ELSE COALESCE(NULLIF(e.data->>'body',''),e.data->>'title','') END,240)
FROM (SELECT DISTINCT ON(scope,conversation_id) scope,conversation_id,kind,data,created_at FROM events WHERE conversation_id<>'' AND kind IN ('message.created','request.created','request.resolved') ORDER BY scope,conversation_id,sequence DESC) e WHERE c.scope=e.scope AND c.id=e.conversation_id;
CREATE INDEX conversations_activity_recent ON conversations(scope,last_activity_at DESC,id DESC);
CREATE FUNCTION update_conversation_activity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE conversations SET last_activity_at=GREATEST(last_activity_at,NEW.created_at),
 last_preview=left(CASE WHEN NEW.kind='request.resolved' THEN COALESCE(NEW.data->'response'->>'outcome','Response received') ELSE COALESCE(NULLIF(NEW.data->>'body',''),NEW.data->>'title','') END,240)
 WHERE scope=NEW.scope AND id=NEW.conversation_id AND last_activity_at<=NEW.created_at;
 RETURN NEW;
END $$;
CREATE TRIGGER conversation_activity AFTER INSERT ON events FOR EACH ROW
 WHEN (NEW.conversation_id<>'' AND NEW.kind IN ('message.created','request.created','request.resolved')) EXECUTE FUNCTION update_conversation_activity();
