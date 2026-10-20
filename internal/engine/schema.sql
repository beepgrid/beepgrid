CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS scopes (
  id text PRIMARY KEY, company_name text NOT NULL, environment text NOT NULL, sequence bigint NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS principals (
  id text PRIMARY KEY, scope text NOT NULL REFERENCES scopes(id), kind text NOT NULL CHECK(kind IN ('person','agent','service')),
  external_id text NOT NULL, name text NOT NULL, metadata jsonb NOT NULL DEFAULT '{}', disabled boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(scope,kind,external_id), UNIQUE(scope,id)
);
CREATE TABLE IF NOT EXISTS credentials (
  id text PRIMARY KEY, scope text NOT NULL, principal_id text NOT NULL, digest text NOT NULL UNIQUE,
  kind text NOT NULL CHECK(kind IN ('admin','api','enrollment','worker','browser')), prefix text NOT NULL DEFAULT '',
  expires_at timestamptz, revoked boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(scope,principal_id) REFERENCES principals(scope,id)
);
CREATE TABLE IF NOT EXISTS instances (
  id text PRIMARY KEY, scope text NOT NULL, agent_id text NOT NULL, credential_id text NOT NULL REFERENCES credentials(id),
  capabilities jsonb NOT NULL DEFAULT '[]', progress jsonb NOT NULL DEFAULT '{}', last_seen timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(), FOREIGN KEY(scope,agent_id) REFERENCES principals(scope,id), UNIQUE(scope,id)
);
CREATE TABLE IF NOT EXISTS groups (
  id text PRIMARY KEY, scope text NOT NULL REFERENCES scopes(id), name text NOT NULL, UNIQUE(scope,id), UNIQUE(scope,name)
);
CREATE TABLE IF NOT EXISTS memberships (
  scope text NOT NULL, group_id text NOT NULL, principal_id text NOT NULL,
  PRIMARY KEY(scope,group_id,principal_id), FOREIGN KEY(scope,group_id) REFERENCES groups(scope,id),
  FOREIGN KEY(scope,principal_id) REFERENCES principals(scope,id)
);
CREATE TABLE IF NOT EXISTS roles (
  id text PRIMARY KEY, scope text NOT NULL REFERENCES scopes(id), name text NOT NULL, rules jsonb NOT NULL DEFAULT '[]',
  UNIQUE(scope,id), UNIQUE(scope,name)
);
CREATE TABLE IF NOT EXISTS role_bindings (
  scope text NOT NULL, role_id text NOT NULL, subject_kind text NOT NULL CHECK(subject_kind IN ('principal','group')),
  subject_id text NOT NULL, PRIMARY KEY(scope,role_id,subject_kind,subject_id), FOREIGN KEY(scope,role_id) REFERENCES roles(scope,id)
);
CREATE TABLE IF NOT EXISTS conversations (
  id text PRIMARY KEY, scope text NOT NULL REFERENCES scopes(id), title text NOT NULL,
  created_by text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(scope,id),
  FOREIGN KEY(scope,created_by) REFERENCES principals(scope,id)
);
CREATE TABLE IF NOT EXISTS participants (
  scope text NOT NULL, conversation_id text NOT NULL, principal_id text NOT NULL,
  PRIMARY KEY(scope,conversation_id,principal_id), FOREIGN KEY(scope,conversation_id) REFERENCES conversations(scope,id),
  FOREIGN KEY(scope,principal_id) REFERENCES principals(scope,id)
);
CREATE TABLE IF NOT EXISTS messages (
  id text PRIMARY KEY, scope text NOT NULL, conversation_id text NOT NULL, sender_id text NOT NULL, instance_id text,
  body text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(scope,conversation_id) REFERENCES conversations(scope,id), FOREIGN KEY(scope,sender_id) REFERENCES principals(scope,id)
);
CREATE INDEX IF NOT EXISTS messages_conversation ON messages(scope,conversation_id,created_at,id);
CREATE TABLE IF NOT EXISTS requests (
  id text PRIMARY KEY, scope text NOT NULL REFERENCES scopes(id), requester_id text NOT NULL, requester_instance text,
  conversation_id text NOT NULL, kind text NOT NULL CHECK(kind IN ('question','approval','task')),
  title text NOT NULL, body text NOT NULL DEFAULT '', choices jsonb NOT NULL DEFAULT '[]', action jsonb,
  state text NOT NULL DEFAULT 'open' CHECK(state IN ('open','resolved','cancelled','expired','superseded')),
  version integer NOT NULL DEFAULT 1, response jsonb, resolved_by text, resolved_at timestamptz,
  expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), shared_key text NOT NULL DEFAULT '',
  FOREIGN KEY(scope,requester_id) REFERENCES principals(scope,id), FOREIGN KEY(scope,conversation_id) REFERENCES conversations(scope,id), UNIQUE(scope,id)
);
CREATE INDEX IF NOT EXISTS requests_open ON requests(scope,state,expires_at);
CREATE TABLE IF NOT EXISTS request_audience (
  scope text NOT NULL, request_id text NOT NULL, principal_id text NOT NULL, conversation_id text NOT NULL,
  PRIMARY KEY(scope,request_id,principal_id), FOREIGN KEY(scope,request_id) REFERENCES requests(scope,id),
  FOREIGN KEY(scope,principal_id) REFERENCES principals(scope,id), FOREIGN KEY(scope,conversation_id) REFERENCES conversations(scope,id)
);
CREATE TABLE IF NOT EXISTS execution_intents (
  id text PRIMARY KEY, scope text NOT NULL, request_id text NOT NULL, executor_id text NOT NULL,
  operation_key text NOT NULL, descriptor jsonb NOT NULL, state text NOT NULL DEFAULT 'authorized',
  result jsonb, updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(scope,request_id),
  FOREIGN KEY(scope,request_id) REFERENCES requests(scope,id), FOREIGN KEY(scope,executor_id) REFERENCES principals(scope,id)
);
CREATE TABLE IF NOT EXISTS connections (
  id text PRIMARY KEY, scope text NOT NULL REFERENCES scopes(id), name text NOT NULL,
  kind text NOT NULL CHECK(kind IN ('webhook','slack','email')), config jsonb NOT NULL, secret bytea NOT NULL,
  enabled boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(scope,id)
);
CREATE TABLE IF NOT EXISTS destinations (
  id text PRIMARY KEY, scope text NOT NULL, connection_id text NOT NULL, principal_id text NOT NULL,
  address text NOT NULL, FOREIGN KEY(scope,connection_id) REFERENCES connections(scope,id),
  FOREIGN KEY(scope,principal_id) REFERENCES principals(scope,id), UNIQUE(scope,connection_id,principal_id,address)
);
CREATE TABLE IF NOT EXISTS deliveries (
  id text PRIMARY KEY, scope text NOT NULL REFERENCES scopes(id), operation_id text NOT NULL, recipient_id text NOT NULL,
  exact_instance text NOT NULL DEFAULT '', connection_id text NOT NULL DEFAULT '', address text NOT NULL DEFAULT '',
  payload jsonb NOT NULL, state text NOT NULL DEFAULT 'queued', attempts integer NOT NULL DEFAULT 0,
  generation bigint NOT NULL DEFAULT 0, owner_instance text NOT NULL DEFAULT '', lease_until timestamptz,
  next_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL, last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(scope,operation_id,recipient_id,connection_id,address),
  FOREIGN KEY(scope,recipient_id) REFERENCES principals(scope,id)
);
CREATE INDEX IF NOT EXISTS deliveries_due ON deliveries(state,next_at,lease_until);
CREATE TABLE IF NOT EXISTS delivery_attempts (
  id text PRIMARY KEY, scope text NOT NULL, delivery_id text NOT NULL REFERENCES deliveries(id), generation bigint NOT NULL,
  outcome text NOT NULL, detail text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS events (
  scope text NOT NULL, sequence bigint NOT NULL, kind text NOT NULL, actor_id text NOT NULL,
  conversation_id text NOT NULL DEFAULT '', recipient_id text NOT NULL DEFAULT '', data jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(scope,sequence)
);
CREATE TABLE IF NOT EXISTS idempotency (
  scope text NOT NULL, principal_id text NOT NULL, operation text NOT NULL, key text NOT NULL,
  input_hash text NOT NULL, output jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(scope,principal_id,operation,key)
);
CREATE TABLE IF NOT EXISTS provider_events (
  scope text NOT NULL, connection_id text NOT NULL, external_id text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(scope,connection_id,external_id)
);
CREATE TABLE IF NOT EXISTS shared_answers (
  id text PRIMARY KEY, scope text NOT NULL REFERENCES scopes(id), key text NOT NULL, context_hash text NOT NULL,
  question text NOT NULL, answer jsonb NOT NULL, author_id text NOT NULL, audience jsonb NOT NULL,
  valid_until timestamptz NOT NULL, invalidated boolean NOT NULL DEFAULT false, source_request text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(scope,id)
);
INSERT INTO schema_migrations(version) VALUES(1) ON CONFLICT DO NOTHING;
ALTER TABLE shared_answers ADD COLUMN IF NOT EXISTS publisher_id text NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS answer_proposals (
 id text PRIMARY KEY, scope text NOT NULL, request_id text NOT NULL, answer_id text NOT NULL, reviewer_id text NOT NULL,
 reason text NOT NULL, state text NOT NULL DEFAULT 'proposed', created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(scope,request_id) REFERENCES requests(scope,id), FOREIGN KEY(scope,answer_id) REFERENCES shared_answers(scope,id)
);
INSERT INTO schema_migrations(version) VALUES(2) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS attachments (
 id text PRIMARY KEY, scope text NOT NULL, conversation_id text NOT NULL, owner_id text NOT NULL,
 file_name text NOT NULL, media_type text NOT NULL, size bigint NOT NULL, sha256 text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), FOREIGN KEY(scope,conversation_id) REFERENCES conversations(scope,id)
);
INSERT INTO schema_migrations(version) VALUES(3) ON CONFLICT DO NOTHING;
