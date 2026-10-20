CREATE TABLE IF NOT EXISTS provider_threads(
 scope text NOT NULL,connection_id text NOT NULL,channel_id text NOT NULL,thread_id text NOT NULL,
 conversation_id text NOT NULL,person_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(scope,connection_id,channel_id,thread_id),
 FOREIGN KEY(scope,connection_id) REFERENCES connections(scope,id),
 FOREIGN KEY(scope,conversation_id) REFERENCES conversations(scope,id),
 FOREIGN KEY(scope,person_id) REFERENCES principals(scope,id)
);
CREATE INDEX IF NOT EXISTS provider_threads_conversation ON provider_threads(scope,connection_id,conversation_id);
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS last_dispatch_at timestamptz NOT NULL DEFAULT 'epoch';
