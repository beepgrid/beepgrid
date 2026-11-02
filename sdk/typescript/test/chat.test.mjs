import {test} from 'node:test';
import assert from 'node:assert/strict';
import {Client} from '../dist/index.js';
const body='👋 @Reviewer 2 please review';
test('StartChat preserves code-point mention offsets, choices, approval and the stable operation key',async()=>{
 const input={targets:[{kind:'agent',id:'a'}],body,approval:true,choices:['Yes','No'],mentions:[{principal_id:'a',start:2,end:13}],attachment_ids:['att_draft']};
 const client=new Client('http://localhost','fixture',async(url,options)=>{
  assert.equal(url,'http://localhost/api/v1/conversations');assert.equal(options.headers['Idempotency-Key'],'shared-1');assert.deepEqual(JSON.parse(options.body),input);
  return Response.json({conversation_id:'shared_1',messages:[]});
 });
 assert.equal((await client.startChat(input,'shared-1')).conversation_id,'shared_1');
});
test('membership keeps a stable key and leaves earlier history opt-in explicit',async()=>{
 let calls=0;const input={operation:'add',version:3,members:['agent_2']};
 const client=new Client('http://localhost','fixture',async(url,options)=>{
  calls++;assert.equal(url,'http://localhost/api/v1/conversations/a%2Fb/membership');assert.equal(options.headers['Idempotency-Key'],'membership-1');assert.deepEqual(JSON.parse(options.body),input);assert.equal(JSON.parse(options.body).share_history,undefined);
  if(calls===1)throw new Error('lost response');return Response.json({conversation_id:'a/b',version:4});
 });
 assert.equal((await client.changeChatMembership('a/b',input,'membership-1')).version,4);assert.equal(calls,2);
});
test('StartChat mentions address a member while all observers receive the original',{skip:!process.env.BEEPGRID_SDK_TEST_URL||!process.env.BEEPGRID_SDK_EXPECT_START_CHAT_MENTIONS},async()=>{
 const owner=new Client(process.env.BEEPGRID_SDK_TEST_URL,process.env.BEEPGRID_SDK_TEST_TOKEN);
 const me=await owner.call('GET','/api/v1/me');
 const reviewer=await owner.connect('sdk/shared-reviewer-'+crypto.randomUUID(),'Reviewer 2');
 const observer=await owner.connect('sdk/shared-observer-'+crypto.randomUUID(),'Observer 3');
 for(const [member,peer] of [[reviewer,observer],[observer,reviewer]]){
  const role=await owner.call('POST','/api/v1/roles',{name:'Shared visibility '+crypto.randomUUID(),rules:[{action:'view',target_kind:'person',target_id:me.person.id},{action:'view',target_kind:'agent',target_id:peer.session.agent.id}]});
  await owner.call('POST','/api/v1/role-bindings',{role_id:role.id,subject_kind:'principal',subject_id:member.session.agent.id});
 }
 const mentions=[{principal_id:reviewer.session.agent.id,start:2,end:13}];
 const sent=await owner.startChat({targets:[{kind:'agent',id:reviewer.session.agent.id},{kind:'agent',id:observer.session.agent.id}],body,mentions},'shared-'+crypto.randomUUID());
 assert.deepEqual(sent.messages[0].mentions,mentions);
 const deliveries=await observer.client.inbox();assert.ok(deliveries.some(d=>d.payload.message?.body===body&&d.payload.conversation_id===sent.conversation_id));
});
