-- Trusted provenance for bounded group creation. Legacy groups are not claimed
-- by whichever delegated user happens to edit them first.
ALTER TABLE groups ADD COLUMN created_by text NOT NULL DEFAULT '';
CREATE INDEX groups_creator ON groups(scope,created_by,id);
-- New clients can retain a positive initial settings revision for CAS. Both
-- optional features remain disabled; untouched version-zero settings had no plans.
UPDATE scopes SET assistance_version=1 WHERE assistance_version=0;
ALTER TABLE scopes ALTER COLUMN assistance_version SET DEFAULT 1;
