#!/usr/bin/env python3
"""Qualify fresh-workspace API journeys against an explicitly local isolated engine.

Run against the dedicated workspace_ui database, never a live tenant. Credentials
stay in .local/workspace-ui-fixtures.json (0600), outside source control.
"""
import json, pathlib, urllib.request, urllib.error, uuid, os, concurrent.futures
BASE=os.environ.get('WORKSPACE_TEST_URL','http://127.0.0.1:4010')
if not BASE.startswith(('http://127.0.0.1:', 'http://localhost:')): raise SystemExit('Local isolated engine required')
root=pathlib.Path(__file__).resolve().parents[2]
owner=(root/'.local/owner-token').read_text().strip()
suffix=uuid.uuid4().hex[:8]
passed=[]
def call(path,body=None,token=owner,key=None,expect=200):
    req=urllib.request.Request(BASE+'/api/v1'+path,data=json.dumps(body).encode() if body is not None else None,headers={'Authorization':'Bearer '+token,'Content-Type':'application/json',**({'Idempotency-Key':key} if key else {})})
    try:
        with urllib.request.urlopen(req,timeout=15) as r: status,value=r.status,json.load(r)
    except urllib.error.HTTPError as e: status,value=e.code,json.load(e)
    assert status==expect, (path,status,value)
    return value

def person(name):return call('/people',{'name':name,'external_id':name.lower().replace(' ','-')+'-'+suffix})
def credential(p):return call('/credentials',{'principal_id':p['id'],'kind':'api','prefix':''})['token']
def role(subject,name,rules):
    r=call('/roles',{'name':name+' '+suffix,'rules':rules})
    call('/role-bindings',{'role_id':r['id'],'subject_kind':'principal','subject_id':subject['id']})
    return r

def check(label,condition):assert condition,label;passed.append(label)
me=call('/me')['person'];alice=person('Taylor Employee');lead=person('Alex Team Lead');hidden=person('Unrelated Person')
agent=call('/sessions',{'agent':'workspace-ui/scout-'+suffix,'name':'Research assistant','capabilities':['message','status'],'metadata':{'project':'Product','description':'Workspace qualification fixture'}})
actor=agent['agent'];worker=agent['token']
role(actor,'Worker replies',[{'action':a,'target_kind':'person','target_id':me['id']} for a in ['view','message','ask']])
role(alice,'View-only employee',[{'action':'view','target_kind':'agent','target_id':actor['id']}])
role(lead,'Bounded team leader',[
 {'action':'view','target_kind':'person','target_id':alice['id']},
 {'action':'manage','target_kind':'person','target_id':alice['id']},
 {'action':'manage','target_kind':'group_creation','target_id':'workspace'},
 {'action':'view','target_kind':'agent','target_id':actor['id']},
])
alice_token,lead_token=credential(alice),credential(lead)
cap=call('/capabilities',{'target':{'kind':'agent','id':actor['id']}},alice_token)
check('View does not imply Message or Ask',cap['operations']['view'] and not cap['operations']['message'] and not cap['operations']['ask'])
call('/messages',{'target':{'kind':'agent','id':actor['id']},'body':'must deny'},alice_token,key='denied-'+suffix,expect=403)
page=call('/query',{'collection':'people'},alice_token)
check('Scoped collection hides unrelated person',hidden['id'] not in json.dumps(page))
cap=call('/capabilities',{'target':{'kind':'person','id':alice['id']}},lead_token)
check('Nonadmin manager has scoped profile management',cap['operations']['manage'])
call('/people/'+alice['id']+'/profile',{'name':'Taylor Employee','department':'Product','email':'','manager_id':lead['id']},lead_token)
call('/people/'+hidden['id']+'/profile',{'name':'must deny'},lead_token,expect=403)
group=call('/groups',{'name':'Product team '+suffix,'members':[alice['id']]},lead_token)
check('Delegated group creation is advertised',call('/capabilities',{'target':{}},lead_token)['operations']['create_group'])
call('/groups',{'id':group['id'],'version':group['version'],'name':'Updated product team '+suffix,'members':[alice['id']]},lead_token)
call('/groups',{'id':group['id'],'version':group['version'],'name':'stale overwrite','members':[alice['id']]},lead_token,expect=409)
passed.append('Stale group edit rejected')
call('/groups',{'name':'Must reject outside team '+suffix,'members':[hidden['id']]},lead_token,expect=403)
passed.append('Group creation cannot escape managed membership bounds')
# The same canonical message must be returned on replay.
body={'target':{'kind':'agent','id':actor['id']},'body':'Replay qualification '+suffix}
sent=call('/messages',body,key='message-'+suffix);replayed=call('/messages',body,key='message-'+suffix)
check('Message retry returns the canonical operation',sent==replayed)
question=call('/requests',{'target':{'kind':'person','id':me['id']},'kind':'question','title':'Qualification question '+suffix,'body':'Choose explicitly','choices':['Brief','Detailed']},worker,key='ask-'+suffix)
check('Request response capabilities include exact eligible outcome',call('/capabilities',{'request_id':question['id'],'target':{}})['outcomes']==['answered'])
# Both replies use the same observed version. The canonical outcome cannot be overwritten.
def respond(answer):
    req=urllib.request.Request(BASE+'/api/v1/requests/'+question['id']+'/responses',data=json.dumps({'version':question['version'],'outcome':'answered','answer':answer}).encode(),headers={'Authorization':'Bearer '+owner,'Content-Type':'application/json'})
    try:
        with urllib.request.urlopen(req) as r:return r.status,json.load(r)
    except urllib.error.HTTPError as e:return e.code,json.load(e)
with concurrent.futures.ThreadPoolExecutor(2) as pool: results=list(pool.map(respond,['Brief','Detailed']))
check('Concurrent decisions preserve one canonical result',sorted(r[0] for r in results)==[200,409])
current=call('/requests/'+question['id']);check('Canonical answer is one submitted exact choice',current['response']['answer'] in ['Brief','Detailed'])
call('/requests/'+question['id'],token=alice_token,expect=404)
passed.append('View-only user cannot read unrelated private request')
call('/conversations/'+sent['conversation_id'],token=lead_token,expect=404)
passed.append('Agent View does not grant another person private conversation')
check('Personal access is readable without admin',call('/access',token=alice_token)['subject']['id']==alice['id'])
# Browser identity accepts Person and rejects worker credentials.
call('/auth/login',{'token':worker},expect=403)
passed.append('Worker credential cannot establish a browser session')
fixtures={'employee':{'person':alice,'token':alice_token},'leader':{'person':lead,'token':lead_token},'agent':actor,'group':group,'private_conversation':sent['conversation_id'],'question':question['id']}
path=root/'.local/workspace-ui-fixtures.json';path.write_text(json.dumps(fixtures));path.chmod(0o600)
print(json.dumps({'checks':len(passed),'passed':passed,'fixtures_file':str(path.relative_to(root))},indent=2))
