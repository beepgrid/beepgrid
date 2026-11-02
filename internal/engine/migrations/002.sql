ALTER TABLE shared_answers ADD COLUMN IF NOT EXISTS publisher_id text NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS answer_proposals (
 id text PRIMARY KEY, scope text NOT NULL, request_id text NOT NULL, answer_id text NOT NULL, reviewer_id text NOT NULL,
 reason text NOT NULL, state text NOT NULL DEFAULT 'proposed', created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(scope,request_id) REFERENCES requests(scope,id), FOREIGN KEY(scope,answer_id) REFERENCES shared_answers(scope,id)
);
