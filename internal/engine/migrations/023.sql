-- Retire semantic matching without rewriting checksummed historical migrations.
-- Release provider alerts held by old jobs before removing their queue.
UPDATE deliveries SET next_at=now()
 WHERE state='queued' AND EXISTS (
  SELECT 1 FROM answer_match_jobs j
   WHERE j.scope=deliveries.scope AND j.request_id=deliveries.operation_id
 );
DROP TABLE answer_match_jobs;
DROP TABLE answer_proposals;
ALTER TABLE scopes DROP COLUMN automatic_answers;
