import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { LiveConnection, liveInput } from '../dist/runtime/index.js';
import { SQLiteInbox } from '../dist/runtime/node.js';
import { codexBinding, piBinding, aiSDKBinding, acpBinding, openCodeBinding, checkpointBinding } from '../dist/runtime/bindings.js';
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const job=(id,conv='con_a',type='message',generation=1)=>({id,generation,payload:{type,conversation_id:conv,message:{id:'msg_'+id,sender_id:'agent_peer',body:'Keep V1',attachments:[{id:'att_private'}]}}});
async function until(f){for(let i=0;i<200;i++){if(f())return;await sleep(10);}throw new Error('Timed out');}
function fixture(t,max){const dir=mkdtempSync(join(tmpdir(),'beepgrid-runtime-'));const path=join(dir,'inbox.db');const store=new SQLiteInbox(path,'workspace/exact-instance',max);t.after(()=>{store.close();rmSync(dir,{recursive:true,force:true});});return {dir,path,store};}
test('durable dedup across generations, exclusive ownership, identity isolation and restart recovery',t=>{
 const {path,store}=fixture(t);store.put(liveInput(job('one')));store.setState('one','submitting');
 store.put(liveInput(job('one','con_a','message',2)));assert.equal(store.pending().length,1);
 assert.throws(()=>new SQLiteInbox(path,'workspace/exact-instance'),/already has/);
 store.close();assert.throws(()=>new SQLiteInbox(path,'another-instance'),/another connection/);
 const recovered=new SQLiteInbox(path,'workspace/exact-instance');assert.equal(recovered.get('one').state,'submitting');assert.equal(statSync(path).mode&0o777,0o600);recovered.close();
});
test('pending limit applies backpressure without discarding previously durable inputs',t=>{
 const {store}=fixture(t,1);store.put(liveInput(job('one')));assert.throws(()=>store.put(liveInput(job('two'))),/full/);assert.equal(store.pending().length,1);store.setState('one','accepted');store.put(liveInput(job('two')));assert.equal(store.pending().length,1);
});
test('intake ACKs only after custody and continues while native submission is busy',async t=>{
 const {store}=fixture(t);const jobs=[job('one'),job('two'),job('three','con_b')];let acked=[];let accepted=[];let release;
 const gate=new Promise(r=>release=r);const client={inbox:async()=>jobs.length?[jobs.shift()]:[],ack:async d=>{assert.ok(store.get(d.id));acked.push(d.id);}};
 const binding={name:'gated',supportsSteer:true,submit:async input=>{accepted.push(input.id);if(input.id==='one')await gate;}};
 const connection=new LiveConnection(client,store,binding).start();await until(()=>acked.length===3 && accepted.includes('three'));
 assert.ok(!accepted.includes('two'));release();await until(()=>store.get('two')?.state==='accepted');connection.stop();await connection.done();
});
test('unknown native acceptance is not automatically replayed or overtaken',async t=>{
 const {store}=fixture(t);store.put(liveInput(job('one')));store.put(liveInput(job('two')));let calls=0;
 const connection=new LiveConnection({inbox:async()=>[],ack:async()=>{}},store,{name:'flaky',supportsSteer:true,submit:async()=>{calls++;throw Error('connection lost');}}).start();
 await until(()=>calls===1);await sleep(150);assert.equal(calls,1);assert.equal(store.get('one').state,'submitting');assert.equal(store.get('two').state,'pending');connection.stop();await connection.done();
});
test('reconciled durable acceptance does not repeat native work',async t=>{
 const {store}=fixture(t);store.put(liveInput(job('one')));store.setState('one','submitting');let calls=0;
 const connection=new LiveConnection({inbox:async()=>[],ack:async()=>{}},store,{name:'host',supportsSteer:true,reconcile:async()=> 'accepted',submit:async()=>{calls++;}}).start();await until(()=>store.get('one').state==='accepted');assert.equal(calls,0);connection.stop();await connection.done();
});
test('request result resumes its waiter even behind Queue work in same session',async t=>{
 const {store}=fixture(t);store.put(liveInput(job('ordinary')));store.put(liveInput(job('result','con_a','request.resolved')));let result=false;let release;const gate=new Promise(r=>release=r);
 const connection=new LiveConnection({inbox:async()=>[],ack:async()=>{}},store,{name:'host',supportsSteer:true,submit:async()=>gate,resume:async input=>{result=input.id==='result';}},'queue').start();await until(()=>result);release();connection.stop();await connection.done();
});
test('revoked credentials stop rather than spinning',async t=>{
 const {store}=fixture(t);const connection=new LiveConnection({inbox:async()=>{throw Object.assign(Error('revoked'),{status:403});},ack:async()=>{}},store,{name:'host',supportsSteer:true,submit:async()=>{}}).start();await assert.rejects(connection.done(),/revoked/);
});
test('binding wire contracts preserve sessions, identifiers, peer and attachment metadata',async()=>{
 let calls=[];const route=async c=>'native_'+c;const rpc=async (...args)=>calls.push(args);const input=liveInput(job('one'));
 await codexBinding({rpc,route,activeTurn:()=> 'turn_1',idle:async()=>{},reconcile:async()=> 'unknown'}).submit(input,'steer');assert.equal(calls[0][0],'turn/steer');assert.equal(calls[0][1].expectedTurnId,'turn_1');assert.equal(calls[0][1].clientUserMessageId,'one');assert.match(calls[0][1].input[0].text,/agent_peer/);assert.match(calls[0][1].input[0].text,/att_private/);
 await piBinding({rpc:async(session,command)=>{assert.equal(session,'native_con_a');assert.equal(command.streamingBehavior,'followUp');assert.equal(command.id,'one');return {success:true};},route}).submit(input,'queue');
 const acp=acpBinding({rpc,route,idle:async()=>{}});assert.equal(acp.supportsSteer,false);await assert.rejects(acp.submit(input,'steer'),/negotiate/);
 const openCode=openCodeBinding({request:rpc,route,idle:async()=>{}});assert.equal(openCode.supportsSteer,false);await openCode.submit(input,'queue');assert.match(calls.at(-1)[0],/native_con_a\/prompt_async/);
});
test('AI SDK final boundary falls back to serialized wake with same ID',async()=>{
 let wakes=[];const binding=aiSDKBinding({route:async()=> 'session',steering:()=>({steer:async(_m,options)=>{assert.equal(options.id,'one');return {type:'not-delivered',reason:'finished'};}}),wake:async(s,i)=>wakes.push(i.id),queue:async()=>{}});
 await binding.submit(liveInput(job('one')),'steer');assert.deepEqual(wakes,['one']);
});
test('custom checkpoint bridge durably appends before wake and routes results separately',async()=>{
 let events=[];const binding=checkpointBinding({route:async c=>c,append:async()=>events.push('append'),wake:async()=>events.push('wake'),resolve:async()=>events.push('resolve')});await binding.submit(liveInput(job('one')),'queue');await binding.resume(liveInput(job('result','con_a','request.resolved')));assert.deepEqual(events,['append','wake','resolve']);
});
test('real PostgreSQL HTTP receive: burst intake, exact instance, approval result, files', {skip:!process.env.NOTIF_SDK_TEST_URL},async t=>{
 const {Client}=await import('../dist/index.js');const owner=new Client(process.env.NOTIF_SDK_TEST_URL,process.env.NOTIF_SDK_TEST_TOKEN);
 const me=await owner.call('GET','/api/v1/me');const {client,session}=await owner.connect('runtime/'+crypto.randomUUID(),'Live runtime',['message','request']);
 await owner.saveContactPermissions({subject:{kind:'agent',id:session.agent.id},version:0,rules:['view','message','ask','decide'].map(action=>({action,target_kind:'person',target_id:me.person.id}))});
 const dir=mkdtempSync(join(tmpdir(),'beepgrid-live-http-'));const store=new SQLiteInbox(join(dir,'inbox.db'),session.instance_id);t.after(()=>{store.close();rmSync(dir,{recursive:true,force:true});});let inputs=[];
 const connection=new LiveConnection(client,store,{name:'checkpoint host',supportsSteer:true,submit:async input=>inputs.push(input),resume:async input=>inputs.push(input)}).start();
 try {
  const chat=await owner.send({target:{kind:'instance',id:session.instance_id},body:'Prepare a handoff'});
  const attachment=await owner.stageAttachment(new Blob(['Private handoff'],{type:'text/plain'}),'handoff.txt');
  await owner.send({conversation_id:chat.conversation_id,body:'Use this reference',attachment_ids:[attachment.id]});
  const request=await client.ask({conversation_id:chat.conversation_id,kind:'approval',body:'May I proceed?'});
  await owner.respond(request.id,{outcome:'approved',version:request.version});
  await until(()=>inputs.some(i=>i.kind==='request.resolved') && inputs.some(i=>i.delivery.payload.message?.attachments?.some(a=>a.id===attachment.id)));
  assert.equal(inputs.filter(i=>i.kind==='request.resolved')[0].delivery.payload.request.response.outcome,'approved');
  assert.equal(new Set(inputs.map(i=>i.id)).size,inputs.length);
  const meMessage=(await owner.conversation(chat.conversation_id)).messages.find(m=>m.body==='Prepare a handoff');
  assert.ok(meMessage.received_by.includes(session.agent.id));
 }finally{connection.stop();await connection.done();}
});
