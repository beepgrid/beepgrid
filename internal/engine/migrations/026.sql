-- Match the full descending key used by opaque coordinator-plan cursors.
DROP INDEX overseer_plans_person;
CREATE INDEX overseer_plans_person ON overseer_plans(scope,person_id,created_at DESC,id DESC);
