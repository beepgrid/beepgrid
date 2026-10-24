-- Retain a safe metric identifier for actionable quota errors.
CREATE OR REPLACE FUNCTION notif_meter_accepted() RETURNS trigger LANGUAGE plpgsql AS $$
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
   RAISE EXCEPTION USING ERRCODE='P0001',MESSAGE='NOTIF_QUOTA_EXCEEDED',DETAIL=TG_ARGV[0];
  END IF;
 END LOOP;
 RETURN NULL;
END $$;
