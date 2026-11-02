WITH input AS MATERIALIZED (
 SELECT value->>'recipient_id' recipient_id,value->>'conversation_id' conversation_id,
 value->>'exact_instance' exact_instance,value->>'delivery_id' delivery_id,
 value->'policy_target' policy_target,(value->>'native')::boolean native,
 value->'providers' providers,value->'request' request,ordinality
 FROM jsonb_array_elements($2::jsonb) WITH ORDINALITY
), reservation AS (
 UPDATE scopes SET sequence=sequence+(SELECT count(*) FROM input) WHERE id=$1 RETURNING sequence
), created AS (
 INSERT INTO conversations(id,scope,title,created_by)
 SELECT conversation_id,$1,$8,$3 FROM input WHERE $9 RETURNING id
), members AS (
 INSERT INTO participants(scope,conversation_id,principal_id)
 SELECT $1,i.conversation_id,i.recipient_id FROM input i JOIN created c ON c.id=i.conversation_id
 UNION SELECT $1,i.conversation_id,$3 FROM input i JOIN created c ON c.id=i.conversation_id
 RETURNING conversation_id
), audiences AS (
 INSERT INTO request_audience(scope,request_id,principal_id,conversation_id,policy_target)
 SELECT $1,$7,i.recipient_id,i.conversation_id,i.policy_target
 FROM input i LEFT JOIN created c ON c.id=i.conversation_id
 WHERE NOT $9 OR c.id IS NOT NULL RETURNING principal_id
), emitted AS (
 INSERT INTO events(scope,sequence,kind,actor_id,conversation_id,recipient_id,data)
 SELECT $1,r.sequence-(SELECT count(*) FROM input)+i.ordinality,'request.created',$3,i.conversation_id,i.recipient_id,i.request
 FROM input i JOIN audiences au ON au.principal_id=i.recipient_id CROSS JOIN reservation r RETURNING sequence
), payloads AS MATERIALIZED (
 SELECT i.*,jsonb_build_object('type','request','request',i.request,'conversation_id',i.conversation_id,
 '_policy',jsonb_build_object('action_name',$10::text,'credential_id',$4::text,'provider_connection',$5::text,
 'sender_id',$3::text,'target',i.policy_target,'action','ask')) payload
 FROM input i JOIN audiences au ON au.principal_id=i.recipient_id
), queued AS (
 INSERT INTO deliveries(id,scope,operation_id,recipient_id,exact_instance,payload,expires_at)
 SELECT delivery_id,$1,$7,recipient_id,exact_instance,payload,$6 FROM payloads WHERE native
 ON CONFLICT DO NOTHING RETURNING id
), external AS (
 INSERT INTO deliveries(id,scope,operation_id,recipient_id,connection_id,address,payload,expires_at,state,routing)
 SELECT d->>'delivery_id',$1,$7,i.recipient_id,d->>'connection_id',d->>'address',i.payload,$6,COALESCE(d->>'state','queued'),COALESCE(d->'routing','{}'::jsonb)
 FROM payloads i CROSS JOIN LATERAL jsonb_array_elements(COALESCE(NULLIF(i.providers,'null'::jsonb),'[]'::jsonb)) d
 ON CONFLICT DO NOTHING RETURNING id
)
SELECT pg_notify('beepgrid_work',json_build_array($1::text,i.recipient_id)::text),pg_notify('beepgrid_events',$1),
 (SELECT pg_notify('beepgrid_provider','') FROM external LIMIT 1)
FROM payloads i
