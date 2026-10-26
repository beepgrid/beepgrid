#!/usr/bin/env python3
"""Generate the checked-in OpenAPI contract. Server route coverage is verified in contract tests."""
import json
import pathlib
import re
root=pathlib.Path(__file__).resolve().parents[1]
S={'type':'string'}
def obj(properties,required=()):return {'type':'object','properties':properties,'required':list(required),'additionalProperties':False}
def ref(name):return {'$ref':'#/components/schemas/'+name}
def array(schema):return {'type':'array','items':schema}
def response(schema):return {'description':'Success','content':{'application/json':{'schema':schema}}}
entity={'id':S,'kind':{'type':'string'},'external_id':S,'name':S,'metadata':{'type':'object'},'disabled':{'type':'boolean'},'created_at':{'type':'string','format':'date-time'}}
schemas={
'Target':obj({'kind':{'enum':['person','agent','service','group','instance']},'id':S},['kind','id']),
'Person':obj({**entity,'kind':{'const':'person'}},['id','kind','name']),
'Agent':obj({**entity,'kind':{'const':'agent'},'instances':array(ref('Instance'))},['id','kind','name']),
'Service':obj({**entity,'kind':{'const':'service'}},['id','kind','name']),
'Instance':obj({'id':S,'agent_id':S,'presence':{'enum':['connected','stale','offline']},'last_seen':S,'capabilities':array(S),'progress':{'type':'object'}}),
'PersonInput':obj({'external_id':S,'name':S,'email':S,'department':S,'manager_id':S},['name']),
'Enrollment':obj({'agent':S,'name':S,'parent_id':S,'capabilities':array(S),'metadata':{'type':'object'}},['agent','name']),
'Session':obj({'token':{'type':'string','writeOnly':False},'agent':ref('Agent'),'instance_id':S,'expires_at':S}),
'Action':obj({'name':S,'executor_id':S,'resource':S,'resource_version':S,'operation_key':S,'arguments':{'type':'object'}},['name','executor_id','resource','resource_version','operation_key']),
'SendInput':obj({'preview_token':S,'target':ref('Target'),'conversation_id':S,'body':{'type':'string','maxLength':32768},'title':S},['body']),
'Message':obj({'id':S,'conversation_id':S,'sender_id':S,'instance_id':S,'body':S,'created_at':S}),
'SendResult':obj({'conversation_id':S,'messages':array(ref('Message'))}),
'SendReceipt':obj({'conversation_id':S,'message_count':{'type':'integer','minimum':1}}),
'Conversation':obj({'id':S,'title':S,'created_by':S,'created_at':S,'participants':array({'type':'object'}),'messages':array(ref('Message'))}),
'AskInput':obj({'preview_token':S,'target':ref('Target'),'conversation_id':S,'kind':{'enum':['question','approval','task']},'title':S,'body':S,'choices':array(S),'action':ref('Action'),'expires_at':S,'shared_key':S},['title']),
'ResponseInput':obj({'outcome':{'enum':['answered','approved','denied','completed','failed','rejected']},'answer':{},'version':{'type':'integer'}},['outcome','version']),
'Response':obj({'responder_instance_id':S,'outcome':S,'answer':{},'version':{'type':'integer'},'source_answer_id':S,'source_invalidated':{'type':'boolean'},'reuse_method':S,'reuse_reason':S}),
'Request':obj({'requester_instance_id':S,'id':S,'requester_id':S,'conversation_id':S,'kind':S,'title':S,'body':S,'choices':array(S),'action':ref('Action'),'state':{'enum':['open','resolved','cancelled','expired','superseded']},'version':{'type':'integer'},'response':ref('Response'),'resolved_by':S,'resolved_at':S,'expires_at':S,'created_at':S,'shared_key':S}),
'Rule':obj({'action':{'enum':['view','message','ask','decide','manage','*']},'target_kind':S,'target_id':S,'connection_id':S,'action_name':S,'conditions':obj({'resource':S,'resource_version':S,'currency':S,'max_amount_minor':{'type':'integer','minimum':0},'before':S})},['action','target_kind','target_id']),
'Role':obj({'version':{'type':'integer','minimum':0},'id':S,'name':S,'rules':array(ref('Rule'))},['name','rules']),
'Group':obj({'created_by':{'type':'string','readOnly':True},'version':{'type':'integer','minimum':0},'member':{'type':'boolean','readOnly':True},'members_complete':{'type':'boolean','readOnly':True},'id':S,'name':S,'members':array(S)},['name','members']),
'Binding':obj({'role_id':S,'subject_kind':{'enum':['principal','group']},'subject_id':S,'remove':{'type':'boolean'}},['role_id','subject_kind','subject_id']),
'Connection':obj({'version':{'type':'integer','minimum':0},'id':S,'name':S,'kind':{'enum':['webhook','slack','email']},'config':{'type':'object','additionalProperties':S},'enabled':{'type':'boolean'},'secret_configured':{'type':'boolean'},'secret':{'type':'string','writeOnly':True}},['name','kind']),
'Destination':obj({'version':{'type':'integer','minimum':0},'id':S,'connection_id':S,'principal_id':S,'address':S,'mode':{'enum':['primary','fallback','copy']},'priority':{'type':'integer'}},['connection_id','principal_id','address']),
'Execution':obj({'id':S,'request_id':S,'executor_id':S,'operation_key':S,'action':ref('Action'),'state':S,'result':{}}),
'Delivery':obj({'id':S,'recipient_id':S,'operation_id':S,'payload':{'type':'object'},'state':S,'generation':{'type':'integer'},'attempts':{'type':'integer'},'last_error':S,'connection_id':S,'address':S,'expires_at':S}),
'Attachment':obj({'id':S,'conversation_id':S,'file_name':S,'media_type':S,'size':{'type':'integer'},'sha256':S,'created_at':S}),
'PublishAnswer':obj({'request_id':S,'key':S,'audience':array(S),'valid_until':S},['request_id','audience']),
'SharedAnswer':{'type':'object','description':'Scoped publication with answer, author/publisher, audience, provenance, validity and invalidation.'},
'AssistanceSettings':obj({'overseer':{'type':'boolean'},'version':{'type':'integer'},'overseer_configured':{'type':'boolean'}}),
'AssistanceUpdate':obj({'version':{'type':'integer','minimum':0},'overseer':{'type':'boolean'}}),
'FleetAgent':obj({'agent':ref('Agent'),'instances':array(ref('Instance')),'fresh':{'type':'boolean'}}),
'OverseerInput':obj({'prompt':{'type':'string','maxLength':4000},'target':ref('Target')},['prompt']),
'OverseerDispatch':obj({'agent_id':S,'conversation_id':S,'request_id':S}),
'OverseerPlan':obj({'id':S,'prompt':S,'context_agent_ids':array(S),'target':ref('Target'),'operation':{'enum':['status','message','question','task']},'summary':S,'body':S,'sources':array(ref('FleetAgent')),'requires_confirmation':{'type':'boolean'},'created_at':S,'expires_at':S,'dispatches':array(ref('OverseerDispatch')),'replies':array(ref('Request')),'reply_summary':S,'reply_digest':S}),
'Event':obj({'sequence':{'type':'integer'},'kind':S,'actor_id':S,'conversation_id':S,'recipient_id':S,'data':{},'created_at':S}),
'Error':obj({'error':obj({'code':S,'message':S},['code','message'])},['error'])}
paths={}
def add(method,path,body=None,result=None,description=''):
 op={'operationId':method.lower()+'_'+re.sub('[^A-Za-z0-9]+','_',path).strip('_'),'description':description or path,'responses':{'200':response(result or {'type':'object'}),'400':response(ref('Error')),'401':response(ref('Error')),'403':response(ref('Error')),'404':response(ref('Error')),'409':response(ref('Error')),'429':response(ref('Error'))},'parameters':[]}
 for name in re.findall(r'{(\w+)}',path):op['parameters'].append({'name':name,'in':'path','required':True,'schema':S})
 if body is not None:op['requestBody']={'required':True,'content':{'application/json':{'schema':body}}}
 if path in ('/api/v1/messages','/api/v1/requests') and method=='POST':op['parameters'].append({'name':'Idempotency-Key','in':'header','required':True,'schema':{'type':'string','minLength':1,'maxLength':200}})
 paths.setdefault(path,{})[method.lower()]=op
for resource,singular in [('people','Person'),('agents','Agent'),('services','Service')]:
 add('GET','/api/v1/'+resource,result=array(ref(singular)))
 add('POST','/api/v1/'+resource+'/{id}/status',obj({'disabled':{'type':'boolean'}},['disabled']))
add('POST','/api/v1/capabilities',obj({'target':obj({'kind':S,'id':S}), 'request_id':S,'connection_id':S,'descriptor':ref('Action')}),obj({'observed_at':S,'operations':{'type':'object','additionalProperties':{'type':'boolean'}},'request_version':{'type':'integer'},'outcomes':array(S)}),description='Current operation eligibility, not an authorization token. Every mutation rechecks current policy. Exact requests include audience, original Ask, reverse response and conditions.')
add('GET','/api/v1/access',result=obj({'subject':{'type':'object'},'basis':S,'roles':array(obj({'role':ref('Role'),'subject_kind':S,'subject_id':S})),'observed_at':S}),description='Own effective access, including direct/group role provenance. Inspecting another subject requires identity visibility and explicit view access:<subject ID>, or an administrator credential.')
paths['/api/v1/access']['get']['parameters'].append({'name':'subject_id','in':'query','schema':S})
add('POST','/api/v1/query',obj({'collection':{'enum':['people','agents','services','groups','roles','connections','destinations','credentials','instances','requests','conversations','messages','answers','executions','deliveries','delivery_attempts','attachments','overseer_plans']},'search':S,'filters':{'type':'object','additionalProperties':S},'limit':{'type':'integer','minimum':1,'maximum':200},'cursor':S},['collection']),obj({'items':array({}),'next_cursor':S,'order':S}),description='Complete bounded keyset reads with current per-record authorization. Continue until next_cursor is absent, including empty pages. Cursors are encrypted and bound to scope, actor, filters and limit for 24 hours. Message history requires conversation_id and is chronological; other collections use stable ascending IDs. Unsupported filters are rejected.')
add('POST','/api/v1/dispatch-preview',obj({'targets':array(ref('Target')),'operation':{'enum':['message','ask']},'descriptor':ref('Action')},['targets','operation']),description='Non-sending preview of 1 to 100 explicit individual/instance targets. Includes native, primary, copy and fallback permission/enablement observations. Token binds the authorized audience and route revisions for ten minutes. Hidden recipient identities and addresses remain private.')
add('POST','/api/v1/message-batches',obj({'targets':array(ref('Target')),'body':S,'title':S,'preview_token':S},['targets','body']),array(obj({'target':ref('Target'),'result':ref('SendResult')})),description='Atomic explicit recipient dispatch, deduplicated by logical identity. Conflicting replica targets are rejected. Private conversations and per-target receipts; failure rolls back the entire batch. Optional preview token prevents changed routes/audience from silently proceeding.')
add('POST','/api/v1/request-batches',obj({'targets':array(ref('Target')),'request':ref('AskInput'),'preview_token':S},['targets','request']),array(obj({'target':ref('Target'),'request':ref('Request')})),description='Atomic private request per explicit target. Each request has an independent response and execution lifecycle; no implicit shared approval or first-responder broadcast. The nested request must omit target and conversation_id.')
for path in ('/api/v1/message-batches','/api/v1/request-batches'):paths[path]['post']['parameters'].append({'name':'Idempotency-Key','in':'header','required':True,'schema':S})
add('GET','/api/v1/integrations/catalog',result=array({'type':'object'}),description='Supported providers and actual fields/capabilities only. Native inbox is built in; no planned integration is installable.')
add('GET','/api/v1/connections/{id}/diagnostics',description='Non-sending configuration/credential checks for a scoped installation manager. Provider acceptance remains not_tested; no external network request is made.')
add('POST','/api/v1/connections/{id}/test',obj({'destination_id':S},['destination_id']),ref('Delivery'),description='Explicit durable test message to one configured destination only. Requires installation/destination management and Message permission. Queue acceptance is not provider acceptance. Inspect delivery evidence afterwards.')
paths['/api/v1/connections/{id}/test']['post']['parameters'].append({'name':'Idempotency-Key','in':'header','required':True,'schema':S})
add('GET','/api/v1/requests/{id}/trace',description='Current authorized request, paged delivery evidence and execution reports. Audience members see their own projection; requesters can inspect their outgoing deliveries. Provider acceptance never proves execution.')
paths['/api/v1/requests/{id}/trace']['get']['parameters'].append({'name':'delivery_cursor','in':'query','schema':S})
add('GET','/api/v1/workspace',description='Authenticated scope, company, environment, deployment kind and schema version. Managed composition supplies deployment metadata explicitly.')
add('GET','/api/v1/workspace/account',description='Person-only scoped account access requires view account:workspace or an administrator credential. This entry point permits status inspection during service suspension with a still-valid credential and current account policy. Runtime-applied quotas/usage are separate from server-side managed requested/provisioning/entitlement state. No operator tokens enter the browser; managed read failures are 503, never zero usage.')
add('POST','/api/v1/roles/preview',obj({'role':ref('Role'),'checks':array(obj({'subject_id':S,'action':S,'target':obj({'kind':S,'id':S}),'connection_id':S,'action_name':S,'descriptor':ref('Action')},['subject_id','action','target']))},['role']),description='Non-saving, version-checked role-edit preview. Same rule ceiling and assignment boundaries as SaveRole; reports exact affected assignments and before/after role-grant checks for authorized subjects. Administrator credential power and new assignments are not simulated. Other assigned roles remain effective.')
add('GET','/api/v1/me')
add('POST','/api/v1/people',ref('PersonInput'),ref('Person'))
add('POST','/api/v1/people/{id}/profile',ref('PersonInput'),ref('Person'))
add('POST','/api/v1/services',obj({'external_id':S,'name':S},['external_id','name']),ref('Service'))
add('POST','/api/v1/sessions',ref('Enrollment'),ref('Session'))
add('GET','/api/v1/agents/{id}/instances',result=array(ref('Instance')))
for name,schema in [('groups','Group'),('roles','Role'),('role-bindings','Binding'),('connections','Connection'),('destinations','Destination')]:
 add('GET','/api/v1/'+name,result=array(ref(schema)))
 add('POST','/api/v1/'+name,ref(schema),ref(schema) if name!='role-bindings' else None)
add('POST','/api/v1/destinations/{id}/remove',obj({}))
add('GET','/api/v1/credentials',result=array({'type':'object','description':'Credential metadata only; no digest or plaintext.'}))
add('POST','/api/v1/credentials',obj({'principal_id':S,'kind':{'enum':['api','enrollment']},'prefix':S},['principal_id','kind']))
add('POST','/api/v1/credentials/{id}/revoke',obj({}))
add('POST','/api/v1/permissions/explain',obj({'subject_id':S,'action':S,'target':obj({'kind':S,'id':S},['kind','id']),'connection_id':S,'action_name':S,'descriptor':ref('Action')},['action','target']))
add('POST','/api/v1/route-preview',obj({'target':ref('Target'),'action':S},['target','action']))
add('GET','/api/v1/conversations',result=array(ref('Conversation')))
add('GET','/api/v1/conversations/{id}',result=ref('Conversation'))
add('POST','/api/v1/messages',ref('SendInput'),ref('SendResult'))
message_op=paths['/api/v1/messages']['post']
message_op['description']='Durably store canonical messages and queue deliveries. Prefer: return=minimal returns a compact acceptance receipt; it is not delivery or execution confirmation. Replay the same input and idempotency key without Prefer to retrieve the full result without resending.'
message_op.setdefault('parameters',[]).append({'name':'Prefer','in':'header','schema':{'type':'string'},'description':'Optional return=minimal; full SendResult remains the default.'})
message_op['responses']['200']['content']['application/json']['schema']={'oneOf':[ref('SendResult'),ref('SendReceipt')]}
message_op['responses']['200']['headers']={'Preference-Applied':{'schema':{'type':'string'},'description':'return=minimal when requested and applied.'}}

add('GET','/api/v1/requests',result=array(ref('Request')))
add('POST','/api/v1/requests',ref('AskInput'),ref('Request'))
add('GET','/api/v1/requests/{id}',result=ref('Request'))
add('POST','/api/v1/requests/{id}/responses',ref('ResponseInput'),ref('Request'))
add('POST','/api/v1/requests/{id}/answer-and-share',obj({'outcome':{'const':'answered'},'answer':{},'version':{'type':'integer'},'audience':array(S)},['outcome','answer','version','audience']),ref('Request'))
for operation in ('cancel','supersede'):add('POST','/api/v1/requests/{id}/'+operation,obj({'version':{'type':'integer'}},['version']),ref('Request'))
add('POST','/api/v1/requests/{id}/redeem',ref('Action'),ref('Execution'))
add('GET','/api/v1/executions',result=array(ref('Execution')))
add('POST','/api/v1/executions/{id}',obj({'state':{'enum':['running','succeeded','failed','unknown']},'result':{}},['state']),ref('Execution'))
add('GET','/api/v1/worker/inbox',result=array(ref('Delivery')))
paths['/api/v1/worker/inbox']['get']['parameters'].append({'name':'wait_ms','in':'query','schema':{'type':'integer','minimum':0,'maximum':20000}})
add('POST','/api/v1/worker/heartbeat',{'type':'object'})
add('POST','/api/v1/worker/inbox/{id}/ack',obj({'generation':{'type':'integer'}},['generation']))
add('GET','/api/v1/deliveries',result=array(ref('Delivery')))
add('POST','/api/v1/deliveries/{id}/retry',obj({}))
for path in ('events','activity'):add('GET','/api/v1/'+path,result=array(ref('Event')))
paths['/api/v1/events']['get']['parameters'] += [{'name':'after','in':'query','schema':{'type':'integer','minimum':0}},{'name':'stream','in':'query','schema':{'enum':['0','1']}}]
paths['/api/v1/events']['get']['parameters'] += [{'name':'summary','in':'query','description':'With stream=1, summary=1 omits event data while preserving current authorization, metadata and replay cursors. Full events remain the default.','schema':{'enum':['0','1']}}]
paths['/api/v1/events']['get']['responses']['200']['content']['text/event-stream']={'schema':{'type':'string'}}
add('GET','/api/v1/answers',result=array(ref('SharedAnswer')))
add('POST','/api/v1/answers',ref('PublishAnswer'),ref('SharedAnswer'))
add('POST','/api/v1/answers/{id}/invalidate',obj({}))
add('GET','/api/v1/assistance/settings',result=ref('AssistanceSettings'))
add('POST','/api/v1/assistance/settings',ref('AssistanceUpdate'),ref('AssistanceSettings'),description='Owner-controlled optional assistance. Both features default off. Enabling requires a configured provider.')
add('POST','/api/v1/fleet/status',obj({'target':ref('Target')}),description='Read-only structured fleet status filtered by current View permission.')
add('POST','/api/v1/fleet/plan',ref('OverseerInput'),ref('OverseerPlan'),description='Optional overseer. Acts only as the caller. Outbound messages, questions and tasks require a saved preview and explicit confirmation.')
add('GET','/api/v1/fleet/plans',result=array(ref('OverseerPlan')))
add('GET','/api/v1/fleet/plans/{id}',result=ref('OverseerPlan'))
add('POST','/api/v1/fleet/plans/{id}/summary',obj({}),ref('OverseerPlan'),description='Read-only summary of currently authorized replies; changed permissions or replies invalidate derived output.')
add('POST','/api/v1/fleet/command',obj({'plan_id':S},['plan_id']),ref('OverseerPlan'),description='Atomically confirm a server-saved preview under current caller permissions. Idempotent per plan. No free-form targets or instructions accepted.')
add('GET','/api/v1/conversations/{id}/attachments',result=array(ref('Attachment')))
add('POST','/api/v1/conversations/{id}/attachments',result=ref('Attachment'))
paths['/api/v1/conversations/{id}/attachments']['post']['requestBody']={'required':True,'content':{'multipart/form-data':{'schema':obj({'file':{'type':'string','format':'binary'}},['file'])}}}
add('GET','/api/v1/attachments/{id}')
paths['/api/v1/attachments/{id}']['get']['responses']['200']={'description':'Authenticated attachment download, at most 5 MiB','content':{'application/octet-stream':{'schema':{'type':'string','format':'binary'}}}}
for path,body in [('slack-account',obj({'team_id':S,'user_id':S},['team_id','user_id'])),('oidc-account',obj({'issuer':S,'subject':S},['issuer','subject']))]:add('POST','/api/v1/people/{id}/'+path,body)
for path in ('/healthz','/readyz','/openapi.json','/api/v1/auth/methods','/auth/oidc/start','/auth/oidc/callback'):
 add('GET',path);paths[path]['get']['security']=[]
for path,body in [('/api/v1/auth/login',obj({'token':S},['token'])),('/api/v1/auth/logout',obj({}))]:
 add('POST',path,body)
paths['/api/v1/auth/login']['post']['security']=[]
for path in ('/integrations/webhook/{id}','/integrations/slack/{id}','/integrations/slack/{id}/events'):
 add('POST',path,ref('SendInput') if 'webhook' in path else {'type':'object'})
 paths[path]['post']['security']=[]
 prefix='X-Notif-' if 'webhook' in path else 'X-Slack-Request-'
 for name in (['X-Notif-Timestamp','X-Notif-Signature','X-Notif-Event-ID'] if 'webhook' in path else ['X-Slack-Request-Timestamp','X-Slack-Signature']):paths[path]['post']['parameters'].append({'name':name,'in':'header','required':True,'schema':S})
paths['/integrations/slack/{id}']['post']['requestBody']={'required':True,'content':{'application/x-www-form-urlencoded':{'schema':obj({'payload':S},['payload'])}}}
for path in ('/auth/oidc/start','/auth/oidc/callback'):paths[path]['get']['responses']['303']={'description':'Redirect after verified login'}
spec={'openapi':'3.1.0','info':{'title':'Beep Grid open source API','version':'0.1.0-dev','license':{'name':'Apache-2.0'}},'servers':[{'url':'/'}], 'security':[{'bearer':[]},{'session':[]}], 'paths':paths,'components':{'schemas':schemas,'securitySchemes':{'bearer':{'type':'http','scheme':'bearer'},'session':{'type':'apiKey','in':'cookie','name':'notif_session'}}}}
(root/'api/openapi.json').write_text(json.dumps(spec,indent=2)+'\n')
print(f'Generated {sum(len(p) for p in paths.values())} API operations.')
