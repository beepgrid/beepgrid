WITH input AS MATERIALIZED (
 SELECT value->>'conversation_id' AS conversation_id,
        value->>'recipient_id' AS recipient_id,
        value->>'delivery_id' AS delivery_id,
        CASE WHEN $5::text<>'' THEN
          jsonb_build_object('id',value->>'message_id','conversation_id',value->>'conversation_id',
            'sender_id',$4::text,'body',$9::text,'created_at',value->'created_at','instance_id',$5::text)
        ELSE
          jsonb_build_object('id',value->>'message_id','conversation_id',value->>'conversation_id',
            'sender_id',$4::text,'body',$9::text,'created_at',value->'created_at') END AS message,
        value->'policy_target' AS policy_target, (value->>'native')::boolean AS native,
        value->'providers' AS providers, ordinality
 FROM jsonb_array_elements($2::jsonb) WITH ORDINALITY
), reservation AS (
 UPDATE scopes SET sequence=sequence+(SELECT count(*) FROM input)
 WHERE id=$1 RETURNING sequence
), created AS (
 INSERT INTO conversations(id,scope,title,created_by)
 SELECT conversation_id,$1,$3,$4 FROM input RETURNING id
), members AS (
 INSERT INTO participants(scope,conversation_id,principal_id)
 SELECT $1,i.conversation_id,i.recipient_id FROM input i JOIN created c ON c.id=i.conversation_id
 UNION SELECT $1,i.conversation_id,$4 FROM input i JOIN created c ON c.id=i.conversation_id
 RETURNING conversation_id
), written AS (
 INSERT INTO messages(id,scope,conversation_id,sender_id,instance_id,body,created_at)
 SELECT i.message->>'id',$1,i.conversation_id,$4,NULLIF($5,''),$9,(i.message->>'created_at')::timestamptz
 FROM input i JOIN created c ON c.id=i.conversation_id RETURNING id
), emitted AS (
 INSERT INTO events(scope,sequence,kind,actor_id,conversation_id,recipient_id,data)
 SELECT $1,r.sequence-(SELECT count(*) FROM input)+i.ordinality,'message.created',$4,i.conversation_id,'',i.message
 FROM input i JOIN written w ON w.id=i.message->>'id' CROSS JOIN reservation r RETURNING sequence
), payloads AS MATERIALIZED (
 SELECT i.conversation_id,i.recipient_id,i.delivery_id,i.native,i.providers,i.message->>'id' AS message_id,
        jsonb_build_object('type','message','message',i.message,'conversation_id',i.conversation_id,
          '_policy',jsonb_build_object('action_name','','credential_id',$6::text,'provider_connection',$7::text,'sender_id',$4::text,'target',i.policy_target,'action','message')) AS payload
 FROM input i JOIN written w ON w.id=i.message->>'id'
), queued AS (
 INSERT INTO deliveries(id,scope,operation_id,recipient_id,payload,expires_at)
 SELECT i.delivery_id,$1,i.message_id,i.recipient_id,i.payload,$8
 FROM payloads i WHERE i.native RETURNING id
), external AS (
 INSERT INTO deliveries(id,scope,operation_id,recipient_id,connection_id,address,payload,expires_at)
 SELECT d->>'delivery_id',$1,i.message_id,i.recipient_id,d->>'connection_id',d->>'address',i.payload,$8
 FROM payloads i CROSS JOIN LATERAL jsonb_array_elements(COALESCE(NULLIF(i.providers,'null'::jsonb),'[]'::jsonb)) d
 ON CONFLICT DO NOTHING RETURNING id
)
SELECT pg_notify('notif_work',json_build_array($1::text,i.recipient_id)::text), pg_notify('notif_events',$1),
       (SELECT pg_notify('notif_provider','') FROM external LIMIT 1)
FROM payloads i
