ALTER TABLE connections DROP CONSTRAINT connections_kind_check;
ALTER TABLE connections ADD CONSTRAINT connections_kind_check CHECK(kind IN ('webhook','slack','email','telegram','discord'));
CREATE UNIQUE INDEX discord_active_application ON connections((config->>'application_id')) WHERE kind='discord' AND enabled;
CREATE UNIQUE INDEX discord_person_identity ON principals(scope,(metadata->>'discord_application_id'),(metadata->>'discord_user_id')) WHERE kind='person' AND metadata->>'discord_user_id' IS NOT NULL;
