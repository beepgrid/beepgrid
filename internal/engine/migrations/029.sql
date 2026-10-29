CREATE FUNCTION initialize_conversation_activity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN NEW.last_activity_at=NEW.created_at; RETURN NEW; END $$;
CREATE TRIGGER conversation_activity_initial BEFORE INSERT ON conversations FOR EACH ROW EXECUTE FUNCTION initialize_conversation_activity();
ALTER TABLE requests ADD COLUMN blocking boolean NOT NULL DEFAULT false;
ALTER TABLE messages ADD COLUMN mentions jsonb NOT NULL DEFAULT '[]';
ALTER TABLE requests ADD COLUMN mentions jsonb NOT NULL DEFAULT '[]';
CREATE INDEX requests_blocking ON requests(scope,requester_id,id) WHERE blocking AND state='open';
ALTER TABLE overseer_plans ADD COLUMN automatic boolean NOT NULL DEFAULT false;
ALTER TABLE overseer_plans ADD COLUMN authority_credential text NOT NULL DEFAULT '';
ALTER TABLE overseer_plans ADD COLUMN summary_done boolean NOT NULL DEFAULT false;
ALTER TABLE overseer_plans ADD COLUMN summary_next_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE overseer_plans ADD COLUMN summary_lease_until timestamptz;
ALTER TABLE overseer_plans ADD COLUMN summary_error text NOT NULL DEFAULT '';
CREATE INDEX coordinator_summary_due ON overseer_plans(summary_next_at) WHERE automatic AND result IS NOT NULL AND NOT summary_done;
