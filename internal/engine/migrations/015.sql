ALTER TABLE scopes ADD COLUMN control_version bigint NOT NULL DEFAULT 0 CHECK(control_version>=0);
