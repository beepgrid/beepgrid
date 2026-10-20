-- Keep completed history out of the hot worker/provider claim paths. No queue
-- row is removed and lease, expiry and exact-instance checks remain in queries.
CREATE INDEX deliveries_native_pending ON deliveries(scope,recipient_id,created_at,id)
  WHERE connection_id='' AND state IN ('queued','leased');
CREATE INDEX deliveries_provider_pending ON deliveries(scope,next_at,id)
  WHERE connection_id<>'' AND state='queued';
CREATE INDEX conversations_recent ON conversations(scope,created_at DESC,id DESC);
CREATE INDEX requests_recent ON requests(scope,created_at DESC,id DESC);
CREATE INDEX participants_by_principal ON participants(scope,principal_id,conversation_id);
