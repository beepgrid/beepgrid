-- Operational suspension is independent of restore reconciliation.
ALTER TABLE scopes ADD COLUMN suspended boolean NOT NULL DEFAULT false;
