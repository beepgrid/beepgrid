-- Destination lanes serialize in-flight sends across dispatchers without
-- serializing independent recipients. Only hot rows belong in these indexes.
CREATE INDEX deliveries_provider_leased_lane ON deliveries(scope,connection_id,address)
  WHERE connection_id<>'' AND state='leased';
CREATE INDEX deliveries_provider_queued_lane ON deliveries(scope,connection_id,address,next_at,id)
  WHERE connection_id<>'' AND state='queued';
