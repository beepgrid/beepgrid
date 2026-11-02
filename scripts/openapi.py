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
'SendInput':{'type': 'object', 'properties': {'preview_token': {'type': 'string'}, 'target': {'$ref': '#/components/schemas/Target'}, 'conversation_id': {'type': 'string'}, 'body': {'type': 'string', 'maxLength': 32768}, 'title': {'type': 'string'}, 'mentions': {'type': 'array', 'items': {'$ref': '#/components/schemas/Mention'}, 'maxItems': 20}, 'reuse_direct': {'type': 'boolean', 'description': 'Reuse authorized exact two-principal history. Defaults to false. Does not merge shared group chats.'}}, 'required': ['body'], 'additionalProperties': False},
'Message':{'type': 'object', 'properties': {'id': {'type': 'string'}, 'conversation_id': {'type': 'string'}, 'sender_id': {'type': 'string'}, 'instance_id': {'type': 'string'}, 'body': {'type': 'string'}, 'created_at': {'type': 'string'}, 'mentions': {'type': 'array', 'items': {'$ref': '#/components/schemas/Mention'}, 'maxItems': 20}}, 'required': [], 'additionalProperties': False},
'SendResult':obj({'conversation_id':S,'messages':array(ref('Message'))}),
'SendReceipt':obj({'conversation_id':S,'message_count':{'type':'integer','minimum':1}}),
'Conversation':{'type': 'object', 'properties': {'id': {'type': 'string'}, 'title': {'type': 'string'}, 'created_by': {'type': 'string'}, 'created_at': {'type': 'string'}, 'participants': {'type': 'array', 'items': {'type': 'object'}}, 'messages': {'type': 'array', 'items': {'$ref': '#/components/schemas/Message'}}, 'last_activity_at': {'type': 'string', 'format': 'date-time'}, 'last_preview': {'type': 'string'}}, 'required': [], 'additionalProperties': False},
'AskInput':{'type': 'object', 'properties': {'preview_token': {'type': 'string'}, 'target': {'$ref': '#/components/schemas/Target'}, 'conversation_id': {'type': 'string'}, 'kind': {'enum': ['question', 'approval', 'task']}, 'title': {'type': 'string'}, 'body': {'type': 'string'}, 'choices': {'type': 'array', 'items': {'type': 'string'}}, 'action': {'$ref': '#/components/schemas/Action'}, 'expires_at': {'type': 'string'}, 'shared_key': {'type': 'string'}, 'mentions': {'type': 'array', 'items': {'$ref': '#/components/schemas/Mention'}, 'maxItems': 20}, 'reuse_direct': {'type': 'boolean', 'description': 'Reuse authorized exact two-principal history. Defaults to false. Does not merge shared group chats.'}, 'blocking': {'type': 'boolean', 'description': 'Explicit agent-to-individual-agent dependency. Ordinary open requests are not deadlocks.'}}, 'required': [], 'additionalProperties': False},
'ResponseInput':obj({'outcome':{'enum':['answered','approved','denied','completed','failed','rejected']},'answer':{},'version':{'type':'integer'}},['outcome','version']),
'Response':obj({'responder_instance_id':S,'outcome':S,'answer':{},'version':{'type':'integer'},'source_answer_id':S,'source_invalidated':{'type':'boolean'},'reuse_method':S,'reuse_reason':S}),
'Request':{'type': 'object', 'properties': {'requester_instance_id': {'type': 'string'}, 'id': {'type': 'string'}, 'requester_id': {'type': 'string'}, 'conversation_id': {'type': 'string'}, 'kind': {'type': 'string'}, 'title': {'type': 'string'}, 'body': {'type': 'string'}, 'choices': {'type': 'array', 'items': {'type': 'string'}}, 'action': {'$ref': '#/components/schemas/Action'}, 'state': {'enum': ['open', 'resolved', 'cancelled', 'expired', 'superseded']}, 'version': {'type': 'integer'}, 'response': {'$ref': '#/components/schemas/Response'}, 'resolved_by': {'type': 'string'}, 'resolved_at': {'type': 'string'}, 'expires_at': {'type': 'string'}, 'created_at': {'type': 'string'}, 'shared_key': {'type': 'string'}, 'mentions': {'type': 'array', 'items': {'$ref': '#/components/schemas/Mention'}, 'maxItems': 20}, 'blocking': {'type': 'boolean', 'description': 'Explicit agent-to-individual-agent dependency. Ordinary open requests are not deadlocks.'}}, 'required': [], 'additionalProperties': False},
'Rule':obj({'effect':{'enum':['allow','deny'],'description':'Omitted means allow. Explicit contact blocks override all matching role grants, except administrator credentials.'},'action':{'enum':['view','message','ask','decide','manage','*']},'target_kind':S,'target_id':S,'connection_id':S,'action_name':S,'conditions':obj({'resource':S,'resource_version':S,'currency':S,'max_amount_minor':{'type':'integer','minimum':0},'before':S})},['action','target_kind','target_id']),
'Role':obj({'version':{'type':'integer','minimum':0},'id':S,'name':S,'rules':array(ref('Rule'))},['name','rules']),
'Group':obj({'icon':{'type':'string','readOnly':True,'description':'Presentation-only role icon. Change with POST /appearance.'},'purpose':{'enum':['contacts','team','agent_role']},'created_by':{'type':'string','readOnly':True},'version':{'type':'integer','minimum':0},'member':{'type':'boolean','readOnly':True},'members_complete':{'type':'boolean','readOnly':True},'id':S,'name':S,'members':array(S)},['name','members']),
'Binding':obj({'role_id':S,'subject_kind':{'enum':['principal','group']},'subject_id':S,'remove':{'type':'boolean'}},['role_id','subject_kind','subject_id']),
'Connection':obj({'version':{'type':'integer','minimum':0},'id':S,'name':S,'kind':{'enum':['webhook','slack','email','telegram','discord']},'config':{'type':'object','additionalProperties':S},'enabled':{'type':'boolean'},'secret_configured':{'type':'boolean'},'secret':{'type':'string','writeOnly':True}},['name','kind']),
'Destination':obj({'version':{'type':'integer','minimum':0},'id':S,'connection_id':S,'principal_id':S,'address':S,'mode':{'enum':['primary','fallback','copy']},'priority':{'type':'integer'}},['connection_id','principal_id','address']),
'Execution':obj({'id':S,'request_id':S,'executor_id':S,'operation_key':S,'action':ref('Action'),'state':S,'result':{}}),
'Delivery':obj({'id':S,'recipient_id':S,'operation_id':S,'payload':{'type':'object'},'state':S,'generation':{'type':'integer'},'attempts':{'type':'integer'},'last_error':S,'connection_id':S,'address':S,'expires_at':S}),
'Attachment':obj({'id':S,'conversation_id':S,'file_name':S,'media_type':S,'size':{'type':'integer'},'sha256':S,'created_at':S}),
'PublishAnswer':obj({'request_id':S,'key':S,'audience':array(S),'valid_until':S},['request_id','audience']),
'SharedAnswer':{'type':'object','description':'Scoped publication with answer, author/publisher, audience, provenance, validity and invalidation.'},
'AssistanceSettings':obj({'overseer':{'type':'boolean'},'version':{'type':'integer'},'overseer_configured':{'type':'boolean'}}),
'AssistanceUpdate':obj({'version':{'type':'integer','minimum':0},'overseer':{'type':'boolean'}}),
'FleetAgent':obj({'agent':ref('Agent'),'instances':array(ref('Instance')),'fresh':{'type':'boolean'}}),
'OverseerInput':obj({'prompt':{'type':'string','maxLength':4000},'target':ref('Target'),'previous_plan_id':{'type':'string'}},['prompt']),
'OverseerDispatch':obj({'agent_id':S,'conversation_id':S,'request_id':S}),
'OverseerPlan':{'type': 'object', 'properties': {'id': {'type': 'string'}, 'prompt': {'type': 'string'}, 'context_agent_ids': {'type': 'array', 'items': {'type': 'string'}}, 'target': {'$ref': '#/components/schemas/Target'}, 'operation': {'enum': ['status', 'message', 'question', 'task']}, 'summary': {'type': 'string'}, 'body': {'type': 'string'}, 'sources': {'type': 'array', 'items': {'$ref': '#/components/schemas/FleetAgent'}}, 'requires_confirmation': {'type': 'boolean'}, 'created_at': {'type': 'string'}, 'expires_at': {'type': 'string'}, 'dispatches': {'type': 'array', 'items': {'$ref': '#/components/schemas/OverseerDispatch'}}, 'replies': {'type': 'array', 'items': {'$ref': '#/components/schemas/Request'}}, 'reply_summary': {'type': 'string'}, 'reply_digest': {'type': 'string'}, 'instructions': {'type': 'array', 'items': {'type': 'object', 'required': ['agent_id', 'operation', 'body'], 'properties': {'agent_id': {'type': 'string'}, 'operation': {'enum': ['message', 'question', 'task']}, 'body': {'type': 'string'}}}}, 'automatic': {'type': 'boolean'}, 'summary_error': {'type': 'string'}}, 'required': [], 'additionalProperties': False},
'Event':obj({'sequence':{'type':'integer'},'kind':S,'actor_id':S,'conversation_id':S,'recipient_id':S,'data':{},'created_at':S}),
'Error':obj({'error':obj({'code':S,'message':S},['code','message'])},['error'])}
schemas['Mention']={'type': 'object', 'required': ['principal_id', 'start', 'end'], 'additionalProperties': False, 'properties': {'principal_id': {'type': 'string'}, 'start': {'type': 'integer', 'minimum': 0}, 'end': {'type': 'integer', 'minimum': 1}}, 'description': 'Stable explicit addressee. start/end are Unicode code-point offsets for the exact @name token. Must be a current visible conversation participant. All members receive the original message and may contribute.'}
schemas['SendInput']['properties']['attachment_ids']={'type':'array','items':S,'maxItems':5,'uniqueItems':True}
schemas['AskInput']['properties']['attachment_ids']={'type':'array','items':S,'maxItems':5,'uniqueItems':True}
for name in ['Message','Request']: schemas[name]['properties']['attachments']=array(ref('Attachment'))
schemas['Message']['properties']['received_by']={'type':'array','items':S,'description':'Read-only native Agent runtime acknowledgement IDs, visible only to the message sender for current Agent participants. Not evidence of model reading or business execution. Provider acceptance is excluded.'}
for name in ['message_id','request_id']: schemas['Attachment']['properties'][name]=S
schemas['Conversation']['properties'].update({'shared':{'type':'boolean'},'version':{'type':'integer'},'manager_id':S,'history_start':{'type':'integer'},'past_participants':array({'type':'object'})})
schemas['ChannelPolicy']=obj({'mode':{'enum':['preferred','all']},'destinations':{'type':'array','items':S,'maxItems':16,'uniqueItems':True},'followup_seconds':{'type':'integer','minimum':0,'maximum':604800,'description':'0 disables unanswered-request follow-ups. Otherwise 60 to 604800 seconds. Only questions and approvals; all mode requires 0.'}},['mode','destinations','followup_seconds'])
schemas['ChannelOverride']=obj({'source':obj({'kind':{'enum':['agent','group']},'id':S},['kind','id']),'policy':ref('ChannelPolicy')},['source','policy'])
schemas['DeliveryPreferences']=obj({'person_id':S,'version':{'type':'integer','minimum':0},'default':{'anyOf':[ref('ChannelPolicy'),{'type':'null'}]},'overrides':{'type':'array','items':ref('ChannelOverride'),'maxItems':64}},['version','default','overrides'])
# Programmatic token lifecycle. Raw secrets appear only in issue/rotate responses.
nullableDate={'type':['string','null'],'format':'date-time'}
schemas['Credential']=obj({'id':S,'principal_id':S,'owner_name':S,'name':S,'kind':S,'prefix':S,'token_hint':S,'created_at':S,'last_used_at':nullableDate,'expires_at':nullableDate,'renewable_until':nullableDate,'previous_until':nullableDate,'permissions':{'type':['array','null'],'items':{'enum':['read','message','request','respond','execute','manage']}},'version':{'type':'integer'},'revoked':{'type':'boolean'},'parent_credential_id':S})
schemas['IssuedCredential']=obj({'id':S,'kind':S,'token':S,'expires_at':S})
schemas['CredentialReplacement']=obj({'token':S,'credential':ref('Credential')})
schemas['Enrollment']['properties'].update({'connection_name':S,'renewable':{'type':'boolean','default':False},'expires_at':{'type':'string','format':'date-time','description':'Hard deadline. Renewable workers use a 24-hour active lease up to this date.'}})
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
add('POST','/api/v1/capabilities',obj({'target':obj({'kind':S,'id':S}), 'request_id':S,'connection_id':S,'descriptor':ref('Action')}),obj({'observed_at':S,'operations':{'type':'object','additionalProperties':{'type':'boolean'}},'request_version':{'type':'integer'},'outcomes':array(S)}),description='Current operation eligibility, not an authorization token. Every mutation rechecks current policy. Exact requests include audience, original Ask, request-scoped replies, explicit blocks and approval conditions.')
add('GET','/api/v1/access',result=obj({'subject':{'type':'object'},'basis':S,'roles':array(obj({'role':ref('Role'),'subject_kind':S,'subject_id':S})),'observed_at':S}),description='Own effective access, including direct/group role provenance. Inspecting another subject requires identity visibility and explicit view access:<subject ID>, or an administrator credential.')
paths['/api/v1/access']['get']['parameters'].append({'name':'subject_id','in':'query','schema':S})
add('POST','/api/v1/query',obj({'collection':{'enum':['people','agents','services','groups','roles','connections','destinations','credentials','instances','requests','conversations','messages','answers','executions','deliveries','delivery_attempts','attachments','overseer_plans']},'search':S,'filters':{'type':'object','additionalProperties':S},'limit':{'type':'integer','minimum':1,'maximum':200},'cursor':S},['collection']),obj({'items':array({}),'next_cursor':S,'order':S}),description='Complete bounded keyset reads with current per-record authorization. Continue until next_cursor is absent, including empty pages. Cursors are encrypted and bound to scope, actor, filters and limit for 24 hours. Message history requires conversation_id and is chronological; other collections use stable ascending IDs. Unsupported filters are rejected.')
add('POST','/api/v1/agents/{id}/contacts',obj({'targets':{'type':'array','items':ref('Target'),'maxItems':100}},['targets']),obj({'contacts':array(obj({'target':ref('Target'),'name':S,'message':{'type':'boolean'},'ask':{'type':'boolean'},'routes':array(obj({'kind':S,'mode':S,'enabled':{'type':'boolean'},'permitted':{'type':'boolean'},'reason':S}))})),'observed_at':S}),description='Non-sending current contact observation for an agent. Requires subject access inspection. Returns only caller-visible contacts that the agent can view and contact. Channel kinds/modes are shown only where the caller can inspect the recipient destination. No addresses, installation IDs, credentials or send authority are returned.')
add('POST','/api/v1/dispatch-preview',obj({'targets':array(ref('Target')),'operation':{'enum':['message','ask']},'descriptor':ref('Action')},['targets','operation']),description='Non-sending preview of 1 to 100 explicit individual/instance targets. Includes native, primary, copy and fallback permission/enablement observations. Token binds the authorized audience and route revisions for ten minutes. Hidden recipient identities and addresses remain private.')
add('POST','/api/v1/message-batches',obj({'targets':array(ref('Target')),'body':S,'title':S,'preview_token':S},['targets','body']),array(obj({'target':ref('Target'),'result':ref('SendResult')})),description='Atomic explicit recipient dispatch, deduplicated by logical identity. Conflicting replica targets are rejected. Private conversations and per-target receipts; failure rolls back the entire batch. Optional preview token prevents changed routes/audience from silently proceeding.')
add('POST','/api/v1/request-batches',obj({'targets':array(ref('Target')),'request':ref('AskInput'),'preview_token':S},['targets','request']),array(obj({'target':ref('Target'),'request':ref('Request')})),description='Atomic private request per explicit target. Each request has an independent response and execution lifecycle; no implicit shared approval or first-responder broadcast. The nested request must omit target and conversation_id.')
for path in ('/api/v1/message-batches','/api/v1/request-batches'):paths[path]['post']['parameters'].append({'name':'Idempotency-Key','in':'header','required':True,'schema':S})
add('GET','/api/v1/integrations/catalog',result=array({'type':'object'}),description='Supported providers and actual fields/capabilities only. Native inbox is built in; no planned integration is installable.')
add('GET','/api/v1/connections/{id}/diagnostics',description='Non-sending credential checks and correlated diagnostic transport evidence. Configuration changes reset test evidence. Provider acceptance is distinct from attributed reply and independent live qualification.')
add('POST','/api/v1/connections/{id}/test',obj({'destination_id':S},['destination_id']),ref('Delivery'),description='Explicit durable test message to one configured destination only. Requires installation/destination management and Message permission. Queue acceptance is not provider acceptance. Inspect delivery evidence afterwards.')
paths['/api/v1/connections/{id}/test']['post']['parameters'].append({'name':'Idempotency-Key','in':'header','required':True,'schema':S})
add('GET','/api/v1/requests/{id}/trace',description='Current authorized request, paged delivery evidence and execution reports. Audience members see their own projection; requesters can inspect their outgoing deliveries. Provider acceptance never proves execution.')
paths['/api/v1/requests/{id}/trace']['get']['parameters'].append({'name':'delivery_cursor','in':'query','schema':S})
add('GET','/api/v1/workspace',description='Authenticated scope, company, environment, deployment kind and schema version. Managed composition supplies deployment metadata explicitly.')
add('GET','/api/v1/workspace/account',description='Person-only scoped account access requires view account:workspace or an administrator credential. This entry point permits status inspection during service suspension with a still-valid credential and current account policy. Runtime-applied quotas/usage are separate from server-side managed requested/provisioning/entitlement state. No operator tokens enter the browser; managed read failures are 503, never zero usage.')
add('POST','/api/v1/roles/preview',obj({'role':ref('Role'),'checks':array(obj({'subject_id':S,'action':S,'target':obj({'kind':S,'id':S}),'connection_id':S,'action_name':S,'descriptor':ref('Action')},['subject_id','action','target']))},['role']),description='Non-saving, version-checked role-edit preview. Same rule ceiling and assignment boundaries as SaveRole; reports exact affected assignments and before/after role-grant checks for authorized subjects. Administrator credential power and new assignments are not simulated. Other assigned roles remain effective.')
add('POST','/api/v1/appearance',obj({'target':ref('Target'),'icon':{'type':'string','description':'mark-0 through mark-11 for an Agent; role-0 through role-11 for a group. Empty resets to its default.'},'version':{'type':'integer','description':'Current group revision. Required for an existing group.'}},['target','icon']),description='Change an Agent or group icon without changing identity, membership or authority. Requires management permission over the target. Group changes are revision checked.')
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
add('GET','/api/v1/people/{id}/delivery-preferences',result=ref('DeliveryPreferences'),description='Read recipient-owned channel preferences. Person self, administrator or identity manager only. Agent override wins over ordered Agent role overrides, then default. Null default retains legacy destinations. Does not grant contact permission.')
add('POST','/api/v1/people/{id}/delivery-preferences',ref('DeliveryPreferences'),ref('DeliveryPreferences'),description='Atomically save versioned recipient defaults and ordered Agent/Agent-role overrides. Destinations must belong to this Person; group selectors must be Agent roles. Preferred tries the first permitted enabled channel, fails over after definite failure, and optionally follows up on unresolved questions/approvals after provider acceptance. All sends selected channels immediately. Empty destinations means workspace only. Saving cancels pending external sends from the old policy; in-flight effects cannot be recalled. Native history stays available. Stale versions conflict.')

add('GET','/api/v1/credentials',result=array(ref('Credential')))
add('POST','/api/v1/credentials',obj({'principal_id':S,'kind':{'enum':['api','enrollment']},'prefix':S,'name':S,'expires_at':{'type':'string','format':'date-time'},'permissions':array({'enum':['read','message','request','respond','execute','manage']})},['principal_id','kind']),ref('IssuedCredential'))
add('POST','/api/v1/credentials/{id}/revoke',obj({'revoke_workers':{'type':'boolean','default':False}}))
paths['/api/v1/credentials/{id}/revoke']['post']['requestBody']['required']=False
add('POST','/api/v1/credentials/{id}/edit',obj({'version':{'type':'integer'},'name':S,'expires_at':{'type':'string','format':'date-time'},'permissions':array(S),'clear_permissions':{'type':'boolean'}},['version']),ref('Credential'))
add('POST','/api/v1/credentials/{id}/rotate',obj({'version':{'type':'integer'},'overlap_seconds':{'type':'integer','minimum':0,'maximum':86400,'default':3600}},['version']),ref('CredentialReplacement'))
add('POST','/api/v1/credentials/self/renew',obj({}),ref('Credential'))
add('POST','/api/v1/permissions/explain',obj({'subject_id':S,'action':S,'target':obj({'kind':S,'id':S},['kind','id']),'connection_id':S,'action_name':S,'descriptor':ref('Action')},['action','target']))
add('POST','/api/v1/route-preview',obj({'target':ref('Target'),'action':S},['target','action']))
add('POST','/api/v1/conversations',obj({'targets':array(ref('Target')),'body':S,'title':S,'approval':{'type':'boolean'},'choices':array(S),'mentions':{'type':'array','items':ref('Mention'),'maxItems':20}},['targets','body']),ref('SendResult'),description='Start an explicit shared chat with a durable first message. Individual recipients only; all members must have current mutual view permission. Every later send checks current contact access. Idempotent; private broadcast contracts are unchanged.')
add('POST','/api/v1/contact-permissions/preview',obj({'subject':ref('Target'),'rules':array(ref('Rule')),'version':{'type':'integer'},'targets':{'type':'array','items':ref('Target'),'maxItems':100}},['subject']),description='Observe effective contact access with direct, group and block provenance. Omit rules to read current access; supply rules and observed version to preview an authorized edit. No writes or grants. Explicit blocks win over matching allows. Returns policy, contacts with action cells, warnings and can_save.')
add('POST','/api/v1/contact-permissions',obj({'subject':ref('Target'),'rules':array(ref('Rule')),'version':{'type':'integer'}},['subject','rules','version']),ref('Role'),description='Atomically save and assign a dedicated contact-access policy. Only view, message, ask and decide toward exact contacts. Existing delegation ceilings and observed-version checks apply. Inherited policies remain effective; an explicit deny overrides matching allows. Omit a direct rule to restore defaults.')
add('POST','/api/v1/conversations/{id}/membership',obj({'operation':{'enum':['rename','add','remove','leave']},'version':{'type':'integer','minimum':1},'title':S,'members':{'type':'array','items':S,'maxItems':100,'uniqueItems':True},'share_history':{'type':'boolean','default':False},'next_manager':S},['operation','version']),obj({'conversation_id':S,'version':{'type':'integer'},'left':{'type':'boolean'}}),description='Versioned and idempotent shared-chat membership. Manager or admin can rename/add/remove; members can leave. New members see history from joining unless share_history is explicitly true. Current mutual View is required; membership grants no contact, decision or tool authority. Removing or leaving stops future read and delivery; already downloaded data cannot be recalled. A departing manager names an existing successor.')
paths['/api/v1/conversations']['post']['requestBody']['content']['application/json']['schema']['properties']['attachment_ids']=schemas['SendInput']['properties']['attachment_ids']
paths['/api/v1/message-batches']['post']['requestBody']['content']['application/json']['schema']['properties']['attachment_ids']=schemas['SendInput']['properties']['attachment_ids']
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
add('POST','/api/v1/requests/{id}/replace',obj({'version':{'type':'integer'},'body':{'type':'string','minLength':1,'maxLength':32768},'mentions':{'type':'array','items':ref('Mention'),'maxItems':20}},['version','body']),obj({'previous':ref('Request'),'replacement':ref('Request')},['previous','replacement']))
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
add('POST','/api/v1/assistance/settings',ref('AssistanceUpdate'),ref('AssistanceSettings'),description='Owner-controlled optional assistance. Enabling requires a configured provider. Coordinator supports automatic dispatch and a separate explicit review flow.')
add('POST','/api/v1/fleet/status',obj({'target':ref('Target')}),description='Read-only structured fleet status filtered by current View permission.')
add('POST','/api/v1/fleet/plan',ref('OverseerInput'),ref('OverseerPlan'),description='Optional overseer. Acts only as the caller. Outbound messages, questions and tasks require a saved preview and explicit confirmation.')
add('GET','/api/v1/fleet/plans',result=array(ref('OverseerPlan')))
add('GET','/api/v1/fleet/plans/{id}',result=ref('OverseerPlan'))
add('POST','/api/v1/fleet/plans/{id}/summary',obj({}),ref('OverseerPlan'),description='Read-only summary of currently authorized replies; changed permissions or replies invalidate derived output.')
add('POST','/api/v1/fleet/command',obj({'plan_id':S},['plan_id']),ref('OverseerPlan'),description='Atomically confirm a server-saved preview under current caller permissions. Idempotent per plan. No free-form targets or instructions accepted.')
add('GET','/api/v1/conversations/{id}/attachments',result=array(ref('Attachment')))
add('POST','/api/v1/conversations/{id}/attachments',result=ref('Attachment'))
paths['/api/v1/conversations/{id}/attachments']['post']['requestBody']={'required':True,'content':{'multipart/form-data':{'schema':obj({'file':{'type':'string','format':'binary'}},['file'])}}}
add('POST','/api/v1/attachments',result=ref('Attachment'),description='Stage a private file for up to 24 hours. Only its uploader can read it until a keyed communication atomically claims attachment_ids. Maximum five files per message, 5 MiB each. An abandoned draft expires; upload itself has no automatic retry contract.')
paths['/api/v1/attachments']['post']['requestBody']=paths['/api/v1/conversations/{id}/attachments']['post']['requestBody']
for path in ['/api/v1/conversations','/api/v1/conversations/{id}/membership','/api/v1/message-batches']:
 paths[path]['post']['parameters'].append({'name':'Idempotency-Key','in':'header','required':True,'schema':{'type':'string','minLength':1,'maxLength':200}})
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
paths['/api/v1/requests/{id}/blocking']={'post': {'operationId': 'setRequestBlocking', 'description': 'Only the requesting agent may declare or clear its own open single-agent blocking dependency. Does not cancel the request.', 'parameters': [{'name': 'id', 'in': 'path', 'required': True, 'schema': {'type': 'string'}}], 'requestBody': {'required': True, 'content': {'application/json': {'schema': {'type': 'object', 'required': ['blocking'], 'additionalProperties': False, 'properties': {'blocking': {'type': 'boolean'}}}}}}, 'responses': {'200': {'description': 'Current request', 'content': {'application/json': {'schema': {'$ref': '#/components/schemas/Request'}}}}, '403': {'description': 'Caller is not the requesting agent'}, '409': {'description': 'Request is no longer open'}}}}
paths['/api/v1/fleet/coordinate']={'post': {'operationId': 'coordinate', 'description': 'Plan and dispatch distinct ordinary communications under current permissions, then durably collect and summarize settled replies. Idempotent recovery. Does not grant business execution authority. Use /fleet/plan and /fleet/command for explicit review.', 'responses': {'200': {'description': 'Success', 'content': {'application/json': {'schema': {'$ref': '#/components/schemas/OverseerPlan'}}}}, '400': {'description': 'Success', 'content': {'application/json': {'schema': {'$ref': '#/components/schemas/Error'}}}}, '401': {'description': 'Success', 'content': {'application/json': {'schema': {'$ref': '#/components/schemas/Error'}}}}, '403': {'description': 'Success', 'content': {'application/json': {'schema': {'$ref': '#/components/schemas/Error'}}}}, '404': {'description': 'Success', 'content': {'application/json': {'schema': {'$ref': '#/components/schemas/Error'}}}}, '409': {'description': 'Success', 'content': {'application/json': {'schema': {'$ref': '#/components/schemas/Error'}}}}, '429': {'description': 'Success', 'content': {'application/json': {'schema': {'$ref': '#/components/schemas/Error'}}}}}, 'parameters': [], 'requestBody': {'required': True, 'content': {'application/json': {'schema': {'$ref': '#/components/schemas/OverseerInput'}}}}}}

schemas['ProviderField']=obj({'name':S,'required':{'type':'boolean'},'secret':{'type':'boolean'}})
schemas['ProviderCapability']=obj({'kind':S,'fields':array(ref('ProviderField')),'inbound_messages':{'type':'boolean'},'native_approvals':{'type':'boolean'},'web_continuation':{'type':'boolean'},'attachments':{'type':'boolean'},'builtin':{'type':'boolean'},'directions':array(S),'replies':{'type':'boolean'},'file_mode':{'enum':['native','workspace_link']},'setup_modes':array(S),'identity':S,'constraints':array(S)})
schemas['ConnectionTestEvidence']=obj({'delivery_id':S,'conversation_id':S,'state':S,'detail':S,'created_at':S,'inbound_reply':{'type':'boolean'}})
schemas['ConnectionDiagnostic']=obj({'connection_id':S,'version':{'type':'integer'},'enabled':{'type':'boolean'},'configuration_valid':{'type':'boolean'},'issues':array(S),'provider_acceptance':{'enum':['not_tested','observed']},'sends':{'const':False},'state':{'enum':['configured','setup_incomplete','disconnected','verifying','action_needed','connected']},'qualification':{'enum':['not_verified','provider_acceptance_observed','engine_round_trip_observed']},'last_test':ref('ConnectionTestEvidence')})
schemas['ProviderSetup']=obj({'connection_id':S,'kind':S,'version':{'type':'integer'},'mode':S,'steps':array(S),'callbacks':{'type':'object','additionalProperties':S},'manifest':{'type':'object'},'registration':{'type':'object'},'diagnostic':ref('ConnectionDiagnostic')})
paths['/api/v1/integrations/catalog']['get']['responses']['200']=response(array(ref('ProviderCapability')))
paths['/api/v1/connections/{id}/diagnostics']['get']['responses']['200']=response(ref('ConnectionDiagnostic'))
add('GET','/api/v1/connections/{id}/setup',result=ref('ProviderSetup'),description='Authorized credential-free setup steps and generated BYO manifest. Never contacts providers or changes registrations.')
add('POST','/api/v1/people/{id}/telegram-account',obj({'bot_id':S,'user_id':S},['bot_id','user_id']),description='Administrator-attested immutable private Telegram identity. Grants no contact or decision authority.')
add('POST','/api/v1/people/{id}/telegram-account/remove',description='Unlink the Person Telegram identity under current account-link authority. Old destinations and callbacks lose authority.')
add('POST','/integrations/telegram/{id}',obj({'update_id':{'type':'integer','minimum':0},'message':{'type':'object'},'callback_query':{'type':'object'}},['update_id']),description='Secret-authenticated private bot updates. Canonical message/decision, thread and dedup receipt commit before success. Same ID with changed content conflicts. See docs/TELEGRAM.md.')
paths['/integrations/telegram/{id}']['post']['security']=[]
paths['/integrations/telegram/{id}']['post']['parameters'].append({'name':'X-Telegram-Bot-Api-Secret-Token','in':'header','required':True,'schema':S})
add('POST','/api/v1/people/{id}/discord-account',obj({'application_id':S,'user_id':S},['application_id','user_id']),description='Administrator-attested immutable Discord application/user identity. Grants no contact or decision authority.')
add('POST','/api/v1/people/{id}/discord-account/remove',description='Unlink the Person Discord identity. Queued sends and callbacks lose authority.')
add('POST','/integrations/discord/{id}',{'type':'object'},description='Ed25519-authenticated Discord interactions. Bot DM commands, reply forms and versioned approvals only. Commits canonical actions and dedup receipt before acknowledgment. See docs/DISCORD.md.')
paths['/integrations/discord/{id}']['post']['security']=[]
for header in ['X-Signature-Timestamp','X-Signature-Ed25519']:
    paths['/integrations/discord/{id}']['post']['parameters'].append({'name':header,'in':'header','required':True,'schema':S})

spec={'openapi':'3.1.0','info':{'title':'Beep Grid open source API','version':'0.1.0-dev','license':{'name':'Apache-2.0'}},'servers':[{'url':'/'}], 'security':[{'bearer':[]},{'session':[]}], 'paths':paths,'components':{'schemas':schemas,'securitySchemes':{'bearer':{'type':'http','scheme':'bearer'},'session':{'type':'apiKey','in':'cookie','name':'notif_session'}}}}
status_descriptions = {'400': 'Invalid input', '401': 'Authentication required', '403': 'Permission denied', '404': 'Not found or not visible', '409': 'Conflict with current state', '413': 'Payload too large', '429': 'Rate limit exceeded', '500': 'Server error', '503': 'Service unavailable'}
for methods in paths.values():
    for operation in methods.values():
        for status, response in operation.get('responses', {}).items():
            if response.get('description') == 'Success' and status in status_descriptions:
                response['description'] = status_descriptions[status]
# Break shared shorthand dictionaries before adding per-field descriptions.
spec=json.loads(json.dumps(spec))
paths=spec['paths'];schemas=spec['components']['schemas']
# Human-reviewed response projections include map-based endpoints and acknowledgments.
for route, shape in json.loads((root/'docs/developer/response-shapes.json').read_text()).items():
    method, path = route.split(' ', 1)
    paths[path][method.lower()]['responses']['200']['content']['application/json']['schema'] = shape
# Human-reviewed usage metadata is shared with the rendered reference.
usage=json.loads((root/'docs/developer/operations.json').read_text())
field_notes=json.loads((root/'docs/developer/fields.json').read_text())
paths['/api/v1/events']['get']['parameters'].append({'name':'conversation_id','in':'query','schema':S,'description':'Optional conversation filter for stream=1 and summary=1; does not narrow the full-payload stream.'})
paths['/api/v1/worker/inbox']['get']['parameters'].append({'name':'wait_ms','in':'query','schema':{'type':'integer','minimum':0,'maximum':20000,'default':0},'description':'Optional long-poll wait in milliseconds.'})
for path, methods in paths.items():
    for method, operation in methods.items():
        note=usage[method.upper()+' '+path]
        operation['summary']=note['description'].split('. ')[0].rstrip('.')
        operation['description']=note['description']+'\n\n'+note['audience']+'\n\nReturns: '+note['returns']+'\n\nRecovery: '+note['errors']
        operation['tags']=[note['family']]
def describe_fields(node):
    if not isinstance(node,dict): return
    for name, field in node.get('properties',{}).items():
        if name in field_notes and not field.get('description'): field['description']=field_notes[name]
        describe_fields(field)
    if isinstance(node.get('items'),dict): describe_fields(node['items'])
schemas['SharedAnswer']=obj({'id':{'type':'string'},'key':{'type':'string'},'context_hash':{'type':'string'},'question':{'type':'string'},'answer':{},'author_id':{'type':'string'},'publisher_id':{'type':'string'},'audience':array({'type':'string'}),'valid_until':{'type':'string','format':'date-time'},'invalidated':{'type':'boolean'},'source_request':{'type':'string'}})
for model in schemas.values(): describe_fields(model)
for methods in paths.values():
    for operation in methods.values():
        for media in operation.get('requestBody',{}).get('content',{}).values(): describe_fields(media.get('schema',{}))
        for reply in operation.get('responses',{}).values():
            for media in reply.get('content',{}).values(): describe_fields(media.get('schema',{}))
# The actual limit counts UTF-8 bytes, which is not JSON Schema maxLength.
schemas['SendInput']['properties']['body'].pop('maxLength',None)
schemas['Rule']['properties']['action']['description']='Directional permission action: view, message, ask, decide, manage or an explicitly granted wildcard.'
paths['/api/v1/route-preview']['post']['requestBody']['content']['application/json']['schema']['properties']['action']['description']='Communication action to inspect, for example message or ask; not a bound business Action descriptor.'
schemas['Request']['properties']['kind']['enum']=['question','approval','task']
schemas['Execution']['properties']['state']['enum']=['authorized','running','succeeded','failed','unknown']
schemas['AskInput']['properties']['kind']['default']='question'
schemas['SendInput']['properties']['reuse_direct']['default']=False
schemas['AskInput']['properties']['reuse_direct']['default']=False
(root/'api/openapi.json').write_text(json.dumps(spec,indent=2)+'\n')
print(f'Generated {sum(len(p) for p in paths.values())} API operations.')
