-- External content is preceded by a durable, scoped upload reservation.
CREATE TABLE attachment_uploads (
 id text PRIMARY KEY,scope text NOT NULL REFERENCES scopes(id),
 lease_until timestamptz NOT NULL DEFAULT now()+interval '15 minutes',
 next_at timestamptz NOT NULL DEFAULT now()+interval '15 minutes'
);
CREATE INDEX attachment_uploads_due ON attachment_uploads(next_at,lease_until);
