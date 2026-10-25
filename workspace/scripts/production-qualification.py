#!/usr/bin/env python3
"""Read-only assertions for completed synthetic browser journeys."""
import json,pathlib,urllib.request,urllib.error
root=pathlib.Path(__file__).resolve().parents[2];base='http://127.0.0.1:4040'
owner=(root/'.local/owner-token').read_text().strip();fixtures=json.loads((root/'.local/workspace-review-fixtures.json').read_text());employee=json.loads((root/'.local/workspace-ui-fixtures.json').read_text())['employee']['token']
def get(path,token=owner,expect=200,body=None):
 req=urllib.request.Request(base+path,data=json.dumps(body).encode() if body is not None else None,headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
 try:
  with urllib.request.urlopen(req,timeout=15) as r: status,raw,headers=r.status,r.read(),r.headers
 except urllib.error.HTTPError as e:status,raw,headers=e.code,e.read(),e.headers
 assert status==expect,(path,status)
 return raw,headers
checks=[]
for q in fixtures['questions']:
 raw,_=get('/api/v1/requests/'+q['id']);data=json.loads(raw)
 assert data['state']=='resolved' and data['response']['answer']=='Brief summary'
checks.append('Both explicit audience questions have canonical answered results')
raw,_=get('/api/v1/requests/'+fixtures['approval']['id']);assert json.loads(raw)['response']['outcome']=='approved';checks.append('Approval decision is durable')
raw,_=get('/api/v1/requests/'+fixtures['task']['id']);assert json.loads(raw)['response']['outcome']=='failed';checks.append('Task failure remains a distinct durable outcome')
conversation=fixtures['questions'][1]['conversation_id']
# Either worker may be the first item in keyset order; use both authorized histories.
found=False
for q in fixtures['questions']:
 raw,_=get('/api/v1/conversations/'+q['conversation_id']);data=json.loads(raw)
 if any('Clarification check:' in m['body'] for m in data.get('messages',[])):found=True
assert found;checks.append('Clarification persists in an original question conversation')
# Verify the browser-created attachment without a public or tokenized URL.
attachments=[]
for q in fixtures['questions']:
 raw,_=get('/api/v1/query',body={'collection':'attachments','filters':{'conversation_id':q['conversation_id']},'limit':100})
 attachments.extend(json.loads(raw)['items'])
attachment=next(a for a in attachments if a['file_name']=='workspace-attachment.txt')
raw,_=get('/api/v1/attachments/'+attachment['id']);assert raw==(root/'.local/workspace-attachment.txt').read_bytes()
_,headers=get('/api/v1/attachments/'+attachment['id'],employee,404)
checks.append('Attachment integrity and unrelated-person denial')
html,headers=get('/agents');assert b'/theme.js' in html and b'<script>' not in html
assert "script-src 'self'" in headers['Content-Security-Policy'];checks.append('Production CSP and external theme bootstrap')
print(json.dumps({'checks':len(checks),'passed':checks},indent=2))
