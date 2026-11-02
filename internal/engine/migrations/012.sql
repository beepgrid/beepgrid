-- Format guard: shared message bodies in private idempotency receipts require
-- version 12 readers. Existing full receipts remain valid and are decoded as-is.
-- No canonical history, queued payload or user configuration is rewritten.
SELECT 1;
