CREATE UNIQUE INDEX IF NOT EXISTS execution_operation_key ON execution_intents(scope,executor_id,operation_key);
