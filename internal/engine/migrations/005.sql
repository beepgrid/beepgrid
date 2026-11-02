ALTER TABLE requests ADD COLUMN IF NOT EXISTS decision_connection text NOT NULL DEFAULT '';
ALTER TABLE request_audience ADD COLUMN IF NOT EXISTS policy_target jsonb;
UPDATE request_audience ra SET policy_target=jsonb_build_object('kind',p.kind,'id',p.id) FROM principals p WHERE p.scope=ra.scope AND p.id=ra.principal_id AND ra.policy_target IS NULL;
ALTER TABLE scopes ADD COLUMN IF NOT EXISTS recovery_hold boolean NOT NULL DEFAULT false;
