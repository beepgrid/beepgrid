ALTER TABLE scopes ADD COLUMN automatic_answers boolean NOT NULL DEFAULT false;
ALTER TABLE scopes ADD COLUMN overseer_enabled boolean NOT NULL DEFAULT false;
ALTER TABLE scopes ADD COLUMN assistance_version bigint NOT NULL DEFAULT 0;

CREATE TABLE answer_match_jobs (
 scope text NOT NULL, request_id text NOT NULL, version bigint NOT NULL, credential_id text NOT NULL DEFAULT '',
 state text NOT NULL DEFAULT 'queued', lease text NOT NULL DEFAULT '',
 lease_until timestamptz NOT NULL DEFAULT 'epoch', attempts integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(scope,request_id),
 FOREIGN KEY(scope,request_id) REFERENCES requests(scope,id)
);
CREATE INDEX answer_match_jobs_ready ON answer_match_jobs(created_at) WHERE state IN ('queued','leased');

CREATE TABLE overseer_plans (
 id text NOT NULL, scope text NOT NULL, person_id text NOT NULL, version bigint NOT NULL,
 plan jsonb NOT NULL, result jsonb, reply_summary text NOT NULL DEFAULT '', reply_digest text NOT NULL DEFAULT '', expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(scope,id),
 FOREIGN KEY(scope,person_id) REFERENCES principals(scope,id)
);

CREATE INDEX overseer_plans_person ON overseer_plans(scope,person_id,created_at DESC);
