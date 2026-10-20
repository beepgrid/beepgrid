ALTER TABLE provider_events ADD COLUMN IF NOT EXISTS input_hash text NOT NULL DEFAULT '';
ALTER TABLE provider_events ADD COLUMN IF NOT EXISTS response jsonb;
