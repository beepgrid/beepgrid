ALTER TABLE destinations ADD COLUMN IF NOT EXISTS mode text NOT NULL DEFAULT 'fallback';
ALTER TABLE destinations ADD COLUMN IF NOT EXISTS priority integer NOT NULL DEFAULT 0;
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM schema_migrations WHERE version=4) THEN
 UPDATE destinations d SET mode='primary' WHERE d.id IN (SELECT DISTINCT ON(scope,principal_id) id FROM destinations ORDER BY scope,principal_id,id);
 END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS destinations_primary ON destinations(scope,principal_id) WHERE mode='primary';
