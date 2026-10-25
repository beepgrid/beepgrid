#!/usr/bin/env python3
"""Create only synthetic review work in the isolated local workspace."""
import pathlib,json,urllib.request,uuid,os
root=pathlib.Path(__file__).resolve().parents[2]
base=os.environ.get('WORKSPACE_TEST_URL','http://127.0.0.1:4040')
if not base.startswith('http://127.0.0.1:'):raise SystemExit('Loopback engine required')
owner=(root/'.local/owner-token').read_text().strip()
def call(path,body=None,token=owner):
 req=urllib.request.Request(base+'/api/v1'+path,data=json.dumps(body).encode() if body is not None else None,headers={'Authorization':'Bearer '+token,'Content-Type':'application/json','Idempotency-Key':uuid.uuid4().hex})
 with urllib.request.urlopen(req,timeout=15) as r:return json.load(r)
me=call('/me')['person'];suffix=uuid.uuid4().hex[:8];workers=[]
for name in ['Verifier','Scheduler']:
 w=call('/sessions',{'agent':'workspace-review/'+name.lower()+'-'+suffix,'name':name,'capabilities':['message','task','status'],'metadata':{'project':'Qualification','description':'Synthetic local review work'}})
 role=call('/roles',{'name':'Review contact '+name+' '+suffix,'rules':[{'action':a,'target_kind':'person','target_id':me['id']} for a in ['view','message','ask']]+[{'action':'ask','target_kind':'agent','target_id':w['agent']['id']}]})
 call('/role-bindings',{'role_id':role['id'],'subject_kind':'principal','subject_id':w['agent']['id']})
 workers.append(w)
questions=[]
for w in workers:
 questions.append(call('/requests',{'target':{'kind':'person','id':me['id']},'kind':'question','title':'Which summary format should we use?','body':'Choose the format for this local qualification report.','choices':['Brief summary','Detailed report'],'shared_key':'workspace-review/'+suffix},w['token']))
action={'name':'qualification.no_external_effect','executor_id':workers[0]['agent']['id'],'resource':'qualification/report','resource_version':'v1','arguments':{'mode':'dry_run','external_effect':False,'untrusted_text':'<script>not executable</script>'},'operation_key':'local-review-'+suffix}
approval=call('/requests',{'target':{'kind':'person','id':me['id']},'kind':'approval','title':'Approve the local dry run?','body':'Synthetic permission test. No external action is performed.','action':action},workers[0]['token'])
task=call('/requests',{'target':{'kind':'person','id':me['id']},'kind':'task','title':'Review the workspace fixture','body':'Record completed, failed or rejected explicitly.'},workers[1]['token'])
path=root/'.local/workspace-review-fixtures.json';path.write_text(json.dumps({'workers':workers,'questions':questions,'approval':approval,'task':task}));path.chmod(0o600)
(root/'.local/workspace-attachment.txt').write_text('Beep Grid synthetic attachment qualification. No customer data.\n')
print(json.dumps({'questions':len(questions),'approval':approval['id'],'task':task['id'],'fixtures':str(path.relative_to(root))}))
