import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Client, NotifError } from '../dist/index.js';
const encode = new TextEncoder();
const event = (sequence, body='héllo') => ({sequence,kind:'message.created',conversation_id:'con_1',data:{id:'msg_'+sequence,body}});
const frame = (n,body) => `id: ${n}\r\ndata: ${JSON.stringify(event(n,body))}\r\n\r\n`;
function response(text, fragmented=false) {
 const bytes=encode.encode(text); let pos=0;
 return new Response(new ReadableStream({pull(c){if(pos>=bytes.length)c.close();else {const end=fragmented?pos+1:bytes.length;c.enqueue(bytes.slice(pos,end));pos=end;}}}),{headers:{'Content-Type':'text/event-stream'}});
}
test('fragmented UTF-8 and CRLF preserve bodies; reconnect resumes complete cursor, skips replay, and accepts cursor-only frames', async()=>{
 const calls=[];const abort=new AbortController();
 const client=new Client('http://localhost','secret',async(url,opts)=>{
  calls.push({url,id:opts.headers['Last-Event-ID']});
  if(calls.length===1)return response(frame(1)+'id: 2\r\nevent: cursor\r\ndata: {}\r\n\r\nid: 3\ndata: {"partial":',true);
  return response(frame(1)+frame(3,'last'));
 });
 const values=[];
 for await(const e of client.events({signal:abort.signal})){values.push(e);if(values.length===2)break;}
 assert.deepEqual(values,[event(1),event(3,'last')]);assert.equal(calls.length,2);assert.equal(calls[1].id,'2');assert.match(calls[1].url,/after=2/);
});
test('revocation is fatal and does not retry or expose provider errors',async()=>{
 let calls=0;const client=new Client('http://localhost','secret',async()=>{calls++;return new Response('private detail',{status:403});});
 await assert.rejects(async()=>{for await(const _ of client.events()){}},e=>e instanceof NotifError&&e.status===403&&!e.message.includes('private'));
 assert.equal(calls,1);
});
test('abort interrupts reconnect backoff and a pending transport',async()=>{
 const abort=new AbortController();let calls=0;
 const client=new Client('http://localhost','',async()=>{calls++;abort.abort();throw new TypeError('offline');});
 for await(const _ of client.events({signal:abort.signal}))assert.fail('unexpected event');
 assert.equal(calls,1);
});
test('invalid cursor, mismatched identity and oversized incomplete frames fail explicitly',async()=>{
 const client=new Client('http://localhost','',async()=>response('id: 4\ndata: '+JSON.stringify(event(3))+'\n\n'));
 await assert.rejects(async()=>{for await(const _ of client.events({after:-1})){}},/cursor/);
 await assert.rejects(async()=>{for await(const _ of client.events()){}},/identity mismatch/);
 const large=new Client('http://localhost','',async()=>response('data: '+'x'.repeat(8*1024*1024+1)));
 await assert.rejects(async()=>{for await(const _ of large.events()){}},/exceeds limit/);
});
test('early iterator return cancels its stream reader',async()=>{
 let cancelled=false;
 const client=new Client('http://localhost','',async()=>new Response(new ReadableStream({start(c){c.enqueue(encode.encode(frame(1)));},cancel(){cancelled=true;}}),{headers:{'Content-Type':'text/event-stream'}}));
 for await(const _ of client.events())break;
 assert.equal(cancelled,true);
});
test('real HTTP full stream preserves private canonical messages without history refetch', {skip:!process.env.NOTIF_SDK_TEST_URL}, async()=>{
 const owner=new Client(process.env.NOTIF_SDK_TEST_URL,process.env.NOTIF_SDK_TEST_TOKEN);
 const {client:worker,session}=await owner.connect('sdk/stream-'+crypto.randomUUID(),'Stream worker');
 const me=await owner.call('GET','/api/v1/me');
 const role=await owner.call('POST','/api/v1/roles',{name:'Stream reader '+crypto.randomUUID(),rules:[{action:'view',target_kind:'person',target_id:me.person.id}]});
 await owner.call('POST','/api/v1/role-bindings',{role_id:role.id,subject_kind:'principal',subject_id:session.agent.id});
 const other=await owner.connect('sdk/stream-other-'+crypto.randomUUID(),'Other worker');
 await owner.send({target:{kind:'agent',id:other.session.agent.id},body:'must stay private'});
 const body='<tag>é\n\\" full event';
 const sent=await owner.send({target:{kind:'agent',id:session.agent.id},body});
 const signal=AbortSignal.timeout(5000);let matched=false;
 for await(const e of worker.events({signal})) {
  assert.notEqual(e.data?.body,'must stay private');
  if(e.conversation_id===sent.conversation_id&&e.kind==='message.created') {
   assert.deepEqual(e.data,sent.messages[0]);matched=true;break;
  }
 }
 assert.equal(matched,true);
});
