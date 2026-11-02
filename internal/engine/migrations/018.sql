-- Opt-in monthly accepted-work budgets. -1 is unlimited; 0 allows no new work.
CREATE TABLE workspace_budgets (
 scope text PRIMARY KEY REFERENCES scopes(id), version bigint NOT NULL CHECK(version>0),
 messages bigint NOT NULL CHECK(messages>=-1), requests bigint NOT NULL CHECK(requests>=-1),
 upload_bytes bigint NOT NULL CHECK(upload_bytes>=-1)
);
CREATE TABLE workspace_usage (
 scope text NOT NULL REFERENCES scopes(id), period date NOT NULL, metric text NOT NULL,
 units bigint NOT NULL CHECK(units>=0), PRIMARY KEY(scope,period,metric),
 CHECK(metric IN ('messages','requests','upload_bytes'))
);
CREATE FUNCTION notif_meter_accepted() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE item record; budget bigint; consumed bigint;
BEGIN
 FOR item IN SELECT n.scope, CASE WHEN TG_ARGV[0]='upload_bytes' THEN sum((to_jsonb(n)->>'size')::bigint) ELSE count(*) END AS units
   FROM accepted_rows n JOIN workspace_budgets b ON b.scope=n.scope GROUP BY n.scope ORDER BY n.scope LOOP
  -- Serialize replicas. Counters and canonical records commit together.
  SELECT (to_jsonb(b)->>TG_ARGV[0])::bigint INTO budget FROM workspace_budgets b WHERE scope=item.scope FOR UPDATE;
  INSERT INTO workspace_usage(scope,period,metric,units)
    VALUES(item.scope,date_trunc('month',CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date,TG_ARGV[0],item.units)
    ON CONFLICT(scope,period,metric) DO UPDATE SET units=workspace_usage.units+excluded.units RETURNING units INTO consumed;
  IF budget>=0 AND consumed>budget THEN
   RAISE EXCEPTION USING ERRCODE='P0001',MESSAGE='NOTIF_QUOTA_EXCEEDED';
  END IF;
 END LOOP;
 RETURN NULL;
END $$;
CREATE TRIGGER meter_messages AFTER INSERT ON messages REFERENCING NEW TABLE AS accepted_rows FOR EACH STATEMENT EXECUTE FUNCTION notif_meter_accepted('messages');
CREATE TRIGGER meter_requests AFTER INSERT ON requests REFERENCING NEW TABLE AS accepted_rows FOR EACH STATEMENT EXECUTE FUNCTION notif_meter_accepted('requests');
CREATE TRIGGER meter_uploads AFTER INSERT ON attachments REFERENCING NEW TABLE AS accepted_rows FOR EACH STATEMENT EXECUTE FUNCTION notif_meter_accepted('upload_bytes');
