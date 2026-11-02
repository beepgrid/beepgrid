-- Reverse lookups stay bounded as credentials, instance history and IAM grow.
CREATE INDEX instances_by_credential ON instances(credential_id);
CREATE INDEX instances_by_agent_recent ON instances(scope,agent_id,created_at DESC,id DESC);
CREATE INDEX role_bindings_by_subject ON role_bindings(scope,subject_kind,subject_id,role_id);
CREATE INDEX memberships_by_principal ON memberships(scope,principal_id,group_id);
