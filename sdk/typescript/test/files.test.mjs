import {test} from 'node:test';
import assert from 'node:assert/strict';
import {Client} from '../dist/index.js';

test('file requests preserve binary bytes, type, path and prevent redirects without retrying uploads',async()=>{
 let requests=[];
 const client=new Client('http://localhost','fixture',async(url,options)=>{
  requests.push({url,options});
  if(options.method==='POST'){
   assert.ok(options.body instanceof FormData);const file=options.body.get('file');assert.equal(file.name,'notes.pdf');assert.equal(await file.text(),'file bytes');
   return Response.json({id:'att_1',file_name:'notes.pdf'});
  }
  return new Response('file bytes',{headers:{'Content-Type':'application/pdf'}});
 });
 assert.equal((await client.uploadAttachment('a/b',new Blob(['file bytes'],{type:'application/pdf'}),'notes.pdf')).id,'att_1');
 const blob=await client.downloadAttachment('a/b');assert.equal(await blob.text(),'file bytes');assert.equal(blob.type,'application/pdf');
 assert.match(requests[0].url,/a%2Fb\/attachments$/);assert.match(requests[1].url,/attachments\/a%2Fb$/);
 assert.ok(requests.every(r=>r.options.redirect==='error'&&r.options.headers.Authorization==='Bearer fixture'));
 let calls=0;const lost=new Client('http://localhost','fixture',async()=>{calls++;throw new Error('lost response')});
 await assert.rejects(()=>lost.uploadAttachment('c',new Blob(['x']),'x'),/lost response/);assert.equal(calls,1);
});

test('real file journey, directory and current isolation',{skip:!process.env.NOTIF_SDK_TEST_URL},async()=>{
 const owner=new Client(process.env.NOTIF_SDK_TEST_URL,process.env.NOTIF_SDK_TEST_TOKEN);
 const {client:worker,session}=await owner.connect('sdk/files-'+crypto.randomUUID(),'File worker');
 const me=await owner.call('GET','/api/v1/me');
 const role=await owner.call('POST','/api/v1/roles',{name:'File reader '+crypto.randomUUID(),rules:[{action:'view',target_kind:'person',target_id:me.person.id}]});
 await owner.call('POST','/api/v1/role-bindings',{role_id:role.id,subject_kind:'principal',subject_id:session.agent.id});
 const other=await owner.connect('sdk/file-other-'+crypto.randomUUID(),'Other worker');
 const sent=await owner.send({target:{kind:'agent',id:session.agent.id},body:'Please review this file'});
 const file=await owner.uploadAttachment(sent.conversation_id,new Blob(['%PDF-1.4\nfixture'],{type:'application/pdf'}),'brief.pdf');
 assert.equal((await worker.attachments(sent.conversation_id))[0].id,file.id);
 assert.equal(await (await worker.downloadAttachment(file.id)).text(),'%PDF-1.4\nfixture');
 await assert.rejects(()=>other.client.downloadAttachment(file.id),e=>[403,404].includes(e.status));
 assert.ok((await owner.directory('agent','File worker')).items.length>=1);
 assert.ok((await owner.directory('person')).items.length>=1);
});
