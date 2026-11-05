CREATE TABLE provider_channel_settings (
 scope text NOT NULL,
 connection_id text NOT NULL,
 address text NOT NULL,
 name text NOT NULL,
 restricted boolean NOT NULL DEFAULT false,
 allowed jsonb NOT NULL DEFAULT '[]',
 version bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(scope, connection_id, address),
 FOREIGN KEY(scope, connection_id) REFERENCES connections(scope, id)
);
