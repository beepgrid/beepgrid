-- Recipient-owned channel policy. An absent default preserves legacy routing.
CREATE TABLE delivery_preferences (
 scope text NOT NULL, person_id text NOT NULL, version bigint NOT NULL DEFAULT 1,
 default_policy jsonb, overrides jsonb NOT NULL DEFAULT '[]',
 PRIMARY KEY(scope,person_id), FOREIGN KEY(scope,person_id) REFERENCES principals(scope,id)
);
-- Private routing metadata never enters message or provider payloads.
ALTER TABLE deliveries ADD COLUMN routing jsonb NOT NULL DEFAULT '{}';
CREATE INDEX deliveries_routing_chain ON deliveries(scope,operation_id,recipient_id)
 WHERE connection_id<>'' AND state IN ('standby','queued');
ALTER TABLE provider_threads ADD COLUMN last_inbound_at timestamptz;
