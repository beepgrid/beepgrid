-- Optional operator-set access deadline. NULL preserves ordinary OSS use.
ALTER TABLE scopes ADD COLUMN access_until timestamptz;
