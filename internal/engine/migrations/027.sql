ALTER TABLE groups ADD COLUMN purpose text NOT NULL DEFAULT 'contacts' CHECK (purpose IN ('contacts','team','agent_role'));

-- Label homogeneous existing collections without changing their membership or
-- permissions. Mixed contact groups keep their existing purpose.
UPDATE groups g SET purpose = CASE
 WHEN NOT EXISTS(SELECT 1 FROM memberships m JOIN principals p ON p.scope=m.scope AND p.id=m.principal_id WHERE m.scope=g.scope AND m.group_id=g.id AND p.kind<>'person') THEN 'team'
 WHEN NOT EXISTS(SELECT 1 FROM memberships m JOIN principals p ON p.scope=m.scope AND p.id=m.principal_id WHERE m.scope=g.scope AND m.group_id=g.id AND p.kind<>'agent') THEN 'agent_role'
 ELSE 'contacts' END
WHERE EXISTS(SELECT 1 FROM memberships m WHERE m.scope=g.scope AND m.group_id=g.id);
