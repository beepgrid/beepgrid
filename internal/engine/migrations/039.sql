-- Capacity belongs to a runtime connection, never to a model-generated status.
ALTER TABLE instances ADD COLUMN anycast_capacity integer NOT NULL DEFAULT 0 CHECK(anycast_capacity BETWEEN 0 AND 100);
ALTER TABLE instances ADD COLUMN anycast_until timestamptz;
ALTER TABLE instances ADD COLUMN anycast_last_assigned timestamptz;
CREATE INDEX instances_anycast_ready ON instances(scope,anycast_until) WHERE anycast_capacity>0;
CREATE TABLE anycast_dispatches (
 id text PRIMARY KEY, scope text NOT NULL REFERENCES scopes(id),
 requester_id text NOT NULL, credential_id text NOT NULL, role_id text NOT NULL,
 input jsonb NOT NULL, state text NOT NULL DEFAULT 'queued'
 CHECK(state IN ('queued','assigned','accepted','completed','cancelled','expired','blocked')),
 agent_id text NOT NULL DEFAULT '', instance_id text NOT NULL DEFAULT '',
 conversation_id text NOT NULL DEFAULT '', request_id text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(), last_error text NOT NULL DEFAULT '',
 FOREIGN KEY(scope,requester_id) REFERENCES principals(scope,id)
);
CREATE INDEX anycast_pending ON anycast_dispatches(scope,updated_at,created_at,id) WHERE state='queued';
CREATE INDEX anycast_work ON anycast_dispatches(updated_at,id,scope) WHERE state IN ('queued','assigned','accepted');
CREATE INDEX anycast_capacity ON anycast_dispatches(scope,instance_id) WHERE state IN ('assigned','accepted');
CREATE INDEX anycast_requester ON anycast_dispatches(scope,requester_id,created_at DESC,id DESC);
CREATE INDEX anycast_due ON anycast_dispatches(expires_at) WHERE state='queued';
