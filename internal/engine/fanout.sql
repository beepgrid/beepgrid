WITH input AS MATERIALIZED (
 SELECT value->>'conversation_id' AS conversation_id,
        value->>'recipient_id' AS recipient_id,
        value->>'delivery_id' AS delivery_id,
        value->'message' AS message,
        value->'policy_target' AS policy_target, ordinality
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
 SELECT i.message->>'id',$1,i.conversation_id,$4,NULLIF($5,''),i.message->>'body',(i.message->>'created_at')::timestamptz
 FROM input i JOIN created c ON c.id=i.conversation_id RETURNING id
), emitted AS (
 INSERT INTO events(scope,sequence,kind,actor_id,conversation_id,recipient_id,data)
 SELECT $1,r.sequence-(SELECT count(*) FROM input)+i.ordinality,'message.created',$4,i.conversation_id,'',i.message
 FROM input i JOIN written w ON w.id=i.message->>'id' CROSS JOIN reservation r RETURNING sequence
), queued AS (
 INSERT INTO deliveries(id,scope,operation_id,recipient_id,payload,expires_at)
 SELECT i.delivery_id,$1,i.message->>'id',i.recipient_id,
        jsonb_build_object('type','message','message',i.message,'conversation_id',i.conversation_id,
          '_policy',jsonb_build_object('action_name','','credential_id',$6::text,'provider_connection',$7::text,'sender_id',$4::text,'target',i.policy_target,'action','message')),$8
 FROM input i JOIN written w ON w.id=i.message->>'id' RETURNING id
)
SELECT pg_notify('notif_work',json_build_array($1::text,i.recipient_id)::text)
FROM input i JOIN queued q ON q.id=i.delivery_id
