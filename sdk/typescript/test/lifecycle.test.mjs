import {test} from 'node:test';
import assert from 'node:assert/strict';
import {Client} from '../dist/index.js';

test('real staged files and shared membership retain history defaults and current access',{skip:!process.env.NOTIF_SDK_TEST_URL||!process.env.NOTIF_SDK_EXPECT_CHAT_LIFECYCLE},async()=>{
 const owner=new Client(process.env.NOTIF_SDK_TEST_URL,process.env.NOTIF_SDK_TEST_TOKEN);
 const me=await owner.call('GET','/api/v1/me');const members=[];
 for(let i=0;i<3;i++)members.push(await owner.connect('sdk/ts-lifecycle/'+crypto.randomUUID(),'TypeScript lifecycle worker'));
 for(const member of members){
  const rules=[{action:'view',target_kind:'person',target_id:me.person.id},...members.filter(peer=>peer!==member).map(peer=>({action:'view',target_kind:'agent',target_id:peer.session.agent.id}))];
  const role=await owner.call('POST','/api/v1/roles',{name:'Lifecycle view '+crypto.randomUUID(),rules});
  await owner.call('POST','/api/v1/role-bindings',{role_id:role.id,subject_kind:'principal',subject_id:member.session.agent.id});
 }
 const reader=members[2].client;const before=await owner.stageAttachment(new Blob(['Before joining'],{type:'text/plain'}),'before.txt');
 await assert.rejects(()=>reader.downloadAttachment(before.id),error=>[403,404].includes(error.status));
 const chat=await owner.startChat({targets:members.slice(0,2).map(member=>({kind:'agent',id:member.session.agent.id})),body:'Before joining',attachment_ids:[before.id]});
 assert.equal(chat.messages[0].attachments[0].id,before.id);
 let changed=await owner.changeChatMembership(chat.conversation_id,{operation:'add',version:1,members:[members[2].session.agent.id]});
 assert.equal(((await reader.conversation(chat.conversation_id)).messages??[]).length,0);
 await assert.rejects(()=>reader.downloadAttachment(before.id),error=>[403,404].includes(error.status));
 const after=await owner.stageAttachment(new Blob(['After joining'],{type:'text/plain'}),'after.txt');
 await owner.send({conversation_id:chat.conversation_id,body:'After joining',attachment_ids:[after.id]});
 assert.equal(await (await reader.downloadAttachment(after.id)).text(),'After joining');
 changed=await owner.changeChatMembership(chat.conversation_id,{operation:'remove',version:changed.version,members:[members[2].session.agent.id]});
 await assert.rejects(()=>reader.downloadAttachment(after.id),error=>[403,404].includes(error.status));
 await owner.changeChatMembership(chat.conversation_id,{operation:'add',version:changed.version,members:[members[2].session.agent.id],share_history:true});
 assert.equal(await (await reader.downloadAttachment(before.id)).text(),'Before joining');
});

test('real request replacement preserves old history and stable retries',{skip:!process.env.NOTIF_SDK_TEST_URL},async()=>{
 const owner=new Client(process.env.NOTIF_SDK_TEST_URL,process.env.NOTIF_SDK_TEST_TOKEN);
 const {session}=await owner.connect('sdk/ts-replace/'+crypto.randomUUID(),'Replacement worker');
 const me=await owner.call('GET','/api/v1/me');
 await owner.saveContactPermissions({subject:{kind:'agent',id:session.agent.id},version:0,rules:['view','decide'].map(action=>({action,target_kind:'person',target_id:me.person.id}))});
 const original=await owner.ask({target:{kind:'agent',id:session.agent.id},body:'Check staging now?',kind:'approval'});
 const input={version:original.version,body:'Check staging after the demo?'};const key=crypto.randomUUID();
 const result=await owner.replaceRequest(original.id,input,key);
 assert.equal(result.previous.state,'superseded');assert.equal(result.replacement.state,'open');assert.notEqual(result.replacement.id,original.id);
 assert.equal((await owner.replaceRequest(original.id,input,key)).replacement.id,result.replacement.id);
});
