ALTER TABLE credentials
 ADD COLUMN name text NOT NULL DEFAULT '',
 ADD COLUMN token_hint text NOT NULL DEFAULT '',
 ADD COLUMN last_used_at timestamptz,
 ADD COLUMN permissions text[],
 ADD COLUMN version bigint NOT NULL DEFAULT 1,
 ADD COLUMN renewable_until timestamptz,
 ADD COLUMN previous_digest text,
 ADD COLUMN previous_until timestamptz,
 ADD COLUMN parent_credential_id text;
CREATE UNIQUE INDEX credentials_previous_digest ON credentials(previous_digest) WHERE previous_digest IS NOT NULL;
CREATE INDEX credentials_parent ON credentials(scope,parent_credential_id) WHERE parent_credential_id IS NOT NULL;
