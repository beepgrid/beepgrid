export type Kind = "person" | "agent" | "service" | "group" | "instance";
export type Target = { kind: Kind; id: string };
export type Identity = {
  id: string;
  kind: "person" | "agent" | "service";
  name: string;
  external_id: string;
  metadata: Record<string, unknown>;
  disabled: boolean;
  created_at: string;
  department?: string;
  manager_id?: string;
  instances?: Instance[];
};
export type Instance = {
  id: string;
  agent_id: string;
  presence: string;
  last_seen: string;
  capabilities: string[];
  progress: Record<string, unknown>;
};
export type Me = {
  person: Identity;
  company: string;
  environment: string;
  admin: boolean;
  can_publish_answers?: boolean;
};
export type Workspace = {
  workspace: {
    company: string;
    environment: string;
    scope: string;
    can_read_account: boolean;
  };
  deployment: string;
  schema_version: number;
  permission_model: { management_targets: Record<string, string> };
};
export type Capability = {
  observed_at: string;
  operations: Record<string, boolean>;
  request_version?: number;
  outcomes: string[];
};
export type Page<T> = { items: T[]; next_cursor?: string; order: string };
export type Action = {
  name: string;
  executor_id: string;
  resource: string;
  resource_version: string;
  arguments: Record<string, unknown>;
  operation_key: string;
};
export type Request = {
  attachments?: Attachment[];
  mentions?: Mention[];
  blocking?: boolean;
  id: string;
  requester_id: string;
  requester_instance_id?: string;
  conversation_id: string;
  kind: "question" | "approval" | "task";
  title: string;
  body: string;
  choices: string[];
  action?: Action;
  state: string;
  version: number;
  created_at: string;
  expires_at: string;
  resolved_at?: string;
  resolved_by?: string;
  shared_key: string;
  response?: {
    outcome: string;
    answer?: unknown;
    source_answer_id?: string;
    source_invalidated?: boolean;
    reuse_method?: string;
    reuse_reason?: string;
  };
};
export type Mention = { principal_id: string; start: number; end: number };
export type Message = {
  received_by?: string[];
  attachments?: Attachment[];
  mentions?: Mention[];
  id: string;
  conversation_id: string;
  sender_id: string;
  instance_id?: string;
  body: string;
  created_at: string;
};
export type Conversation = {
  shared?: boolean;
  version: number;
  manager_id?: string;
  history_start?: number;
  past_participants?: Identity[];
  last_activity_at?: string;
  last_preview?: string;
  id: string;
  title: string;
  created_by: string;
  created_at: string;
  participants: Identity[];
  messages?: Message[];
};
export type Group = {
  icon?: string;
  purpose?: "team" | "agent_role" | "contacts";
  id: string;
  name: string;
  members: string[];
  members_complete: boolean;
  member: boolean;
  version: number;
  created_by: string;
};
export type Rule = {
  effect?: "allow" | "deny";
  action: string;
  target_kind: string;
  target_id: string;
  connection_id?: string;
  action_name?: string;
  conditions?: {
    resource?: string;
    resource_version?: string;
    currency?: string;
    max_amount_minor?: number;
    before?: string;
  };
};
export type Role = { id: string; name: string; rules: Rule[]; version: number };
export type Binding = {
  role_id: string;
  subject_kind: "principal" | "group";
  subject_id: string;
};
export type Access = {
  subject: Identity;
  basis: string;
  observed_at: string;
  roles: { role: Role; subject_kind: string; subject_id: string }[];
};
export type Connection = {
  id: string;
  name: string;
  kind: string;
  config: Record<string, string>;
  enabled: boolean;
  secret_configured: boolean;
  version: number;
};
export type Destination = {
  id: string;
  connection_id: string;
  principal_id: string;
  address: string;
  mode: string;
  priority: number;
  version: number;
};
export type Credential = {
  name: string;
  owner_name: string;
  token_hint: string;
  created_at: string;
  last_used_at?: string;
  permissions?: string[] | null;
  version: number;
  renewable_until?: string;
  previous_until?: string;
  parent_credential_id?: string;
  id: string;
  principal_id: string;
  kind: string;
  prefix: string;
  expires_at?: string;
  revoked: boolean;
};
export type Attachment = {
  message_id?: string;
  request_id?: string;
  id: string;
  file_name: string;
  media_type: string;
  size: number;
  created_at: string;
  conversation_id: string;
  sha256: string;
};
export type PublishedAnswer = {
  id: string;
  key: string;
  question: string;
  answer: unknown;
  author_id: string;
  publisher_id: string;
  audience: string[];
  valid_until: string;
  source_request: string;
  invalidated: boolean;
};
export type AssistanceSettings = {
  overseer: boolean;
  overseer_configured: boolean;
  version: number;
};
export type FleetSource = {
  agent: Identity;
  instances: Instance[];
  fresh: boolean;
};
export type Plan = {
  automatic?: boolean;
  instructions?: { agent_id: string; operation: string; body: string }[];
  summary_error?: string;
  previous_plan_id?: string;
  target?: Target;
  id: string;
  prompt: string;
  operation: string;
  body?: string;
  summary: string;
  sources: FleetSource[];
  requires_confirmation: boolean;
  expires_at: string;
  created_at: string;
  dispatches: {
    agent_id: string;
    conversation_id: string;
    request_id?: string;
  }[];
  replies: Request[];
  reply_summary?: string;
};
export type Preview = {
  recipients: {
    target: Target;
    name: string;
    routes: {
      kind: string;
      mode: string;
      enabled: boolean;
      permitted: boolean;
      reason: string;
    }[];
  }[];
  count: number;
  hidden_recipients: number;
  token: string;
  expires_at: string;
  sends: false;
};

export type ChannelPolicy = {
  mode: "preferred" | "all";
  destinations: string[];
  followup_seconds: number;
};
export type ChannelOverride = { source: Target; policy: ChannelPolicy };
export type DeliveryPreferences = {
  person_id: string;
  version: number;
  default: ChannelPolicy | null;
  overrides: ChannelOverride[];
};
