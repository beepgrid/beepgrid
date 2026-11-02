CREATE INDEX IF NOT EXISTS delivery_attempts_history ON delivery_attempts(scope,delivery_id,created_at,id);
