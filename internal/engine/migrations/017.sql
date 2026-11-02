-- Tombstones survive customer-data deletion and reject stale activation.
ALTER TABLE scopes ADD COLUMN erasure_operation text NOT NULL DEFAULT '';
ALTER TABLE scopes ADD COLUMN erase_after timestamptz;
ALTER TABLE scopes ADD COLUMN erasure_started_at timestamptz;
ALTER TABLE scopes ADD COLUMN erased_at timestamptz;
ALTER TABLE scopes ADD COLUMN erasure_next_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX workspace_erasure_due ON scopes(erasure_next_at,erase_after) WHERE erasure_operation<>'' AND erased_at IS NULL;
