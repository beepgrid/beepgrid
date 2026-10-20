CREATE TABLE IF NOT EXISTS attachments (
 id text PRIMARY KEY, scope text NOT NULL, conversation_id text NOT NULL, owner_id text NOT NULL,
 file_name text NOT NULL, media_type text NOT NULL, size bigint NOT NULL, sha256 text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), FOREIGN KEY(scope,conversation_id) REFERENCES conversations(scope,id)
);
