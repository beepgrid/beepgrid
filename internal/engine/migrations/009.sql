CREATE INDEX IF NOT EXISTS requests_shared_open ON requests(scope,shared_key,created_at) WHERE kind='question' AND state='open' AND shared_key<>'';
