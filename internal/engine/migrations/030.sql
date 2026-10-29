ALTER TABLE connections DROP CONSTRAINT connections_kind_check;
ALTER TABLE connections ADD CONSTRAINT connections_kind_check CHECK(kind IN ('webhook','slack','email','telegram'));
CREATE UNIQUE INDEX telegram_active_bot ON connections((config->>'bot_id')) WHERE kind='telegram' AND enabled;
CREATE UNIQUE INDEX telegram_person_identity ON principals(scope,(metadata->>'telegram_bot_id'),(metadata->>'telegram_user_id')) WHERE kind='person' AND metadata->>'telegram_user_id' IS NOT NULL;
