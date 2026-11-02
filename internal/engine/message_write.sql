WITH input AS MATERIALIZED (
 SELECT value->>'recipient_id' AS recipient_id, value->>'exact_instance' AS exact_instance,
        value->>'delivery_id' AS delivery_id, value->'policy_target' AS policy_target,
        (value->>'native')::boolean AS native, value->'providers' AS providers
 FROM jsonb_array_elements($3::jsonb)
), reservation AS (
 UPDATE scopes SET sequence=sequence+1 WHERE id=$1 RETURNING sequence
), written AS (
 INSERT INTO messages(id,scope,conversation_id,sender_id,instance_id,body,created_at,mentions,created_sequence)
 SELECT $2::jsonb->>'id',$1,$2::jsonb->>'conversation_id',$2::jsonb->>'sender_id',
        NULLIF($2::jsonb->>'instance_id',''),$2::jsonb->>'body',($2::jsonb->>'created_at')::timestamptz,COALESCE($2::jsonb->'mentions','[]'::jsonb),reservation.sequence
 FROM reservation
 RETURNING id
), emitted AS (
 INSERT INTO events(scope,sequence,kind,actor_id,conversation_id,recipient_id,data)
 SELECT $1,r.sequence,'message.created',$2::jsonb->>'sender_id',$2::jsonb->>'conversation_id','',$2::jsonb
 FROM written CROSS JOIN reservation r RETURNING sequence
), payloads AS MATERIALIZED (
 SELECT i.*,jsonb_build_object('type','message','message',$2::jsonb,'conversation_id',$2::jsonb->>'conversation_id',
 '_policy',jsonb_build_object('action_name','','credential_id',$4::text,'provider_connection',$5::text,
 'sender_id',$2::jsonb->>'sender_id','target',i.policy_target,'action','message')) AS payload
 FROM input i CROSS JOIN written
), queued AS (
 INSERT INTO deliveries(id,scope,operation_id,recipient_id,exact_instance,payload,expires_at)
 SELECT delivery_id,$1,$2::jsonb->>'id',recipient_id,exact_instance,payload,$6 FROM payloads WHERE native
 ON CONFLICT DO NOTHING RETURNING id
), external AS (
 INSERT INTO deliveries(id,scope,operation_id,recipient_id,connection_id,address,payload,expires_at,state,routing)
 SELECT d->>'delivery_id',$1,$2::jsonb->>'id',i.recipient_id,d->>'connection_id',d->>'address',i.payload,$6,COALESCE(d->>'state','queued'),COALESCE(d->'routing','{}'::jsonb)
 FROM payloads i CROSS JOIN LATERAL jsonb_array_elements(COALESCE(NULLIF(i.providers,'null'::jsonb),'[]'::jsonb)) d
 ON CONFLICT DO NOTHING RETURNING id
)
SELECT pg_notify('beepgrid_work',json_build_array($1::text,i.recipient_id)::text),pg_notify('beepgrid_events',$1),
       (SELECT pg_notify('beepgrid_provider','') FROM external LIMIT 1)
FROM payloads i
