-- Access grants have their own revision and cannot override security suspension.
ALTER TABLE scopes ADD COLUMN access_version bigint NOT NULL DEFAULT 0;
