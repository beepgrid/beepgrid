# Self-host deployment and recovery

## Existing PostgreSQL

Use PostgreSQL 17, an isolated schema, a dedicated runtime role, HTTPS at your reverse proxy, durable attachment storage and a backed-up 32-byte encryption key. The local Compose database uses development credentials and binds only to loopback. It is not a production database configuration.

Generate and store `NOTIF_BOOTSTRAP_TOKEN` and `NOTIF_SECRET_KEY` through your secret manager. The latter is exactly 64 hexadecimal characters. Never commit them. Configure:

```sh
export NOTIF_DATABASE_URL='postgres://runtime-user:password@db/notif?sslmode=verify-full'
export NOTIF_SCHEMA=notif
export NOTIF_WORKSPACE=acme
export NOTIF_ENVIRONMENT=production
export NOTIF_COMPANY=Acme
export NOTIF_ADDR=127.0.0.1:4010
export NOTIF_PUBLIC_URL=https://notify.example.com
export NOTIF_ATTACHMENTS_DIR=/var/lib/notif/attachments
```

Run `notif migrate` with a separate deployment credential permitted to create/alter the schema. Then use a runtime role with schema USAGE and SELECT/INSERT/UPDATE/DELETE on its tables. Serving calls `OpenExisting` and requires no DDL. Apply equivalent default privileges for future migrations. The bootstrap credential initializes a new workspace once; restarting an existing deployment must supply a currently valid owner credential. Provider encryption keys must match the existing encrypted data.

Run `notif serve`. `/healthz` confirms the process; `/readyz` checks database connectivity. The production binary embeds the UI. Set proxy idle/response timeouts above the 25-second SSE cycle and 20-second long-poll maximum. Keep request origins consistent with `NOTIF_PUBLIC_URL`. Do not trust forwarded IP headers for authorization.

A single process is the default supported topology. Database-backed queues and notification hints allow multiple engine processes, but all must use the same schema, encryption key, public origin and shared attachment storage. Full HA, load capacity, shared storage behavior and regional operations need deployment-specific qualification.

## Docker image

```sh
docker build -t notif:local .
docker run --rm notif:local version
NOTIF_TEST_IMAGE=notif:local python3 scripts/container-smoke.py
```

The final image is non-root and has no shell. Pass configuration using a protected env file or your orchestrator's secret integration. Mount `/data` as a writable durable volume. Run the same image with the `migrate` command before the long-running `serve` command. When connecting to an existing database, do not assume container `localhost` means the host database.

## Backups and upgrades

Back up PostgreSQL using a matching-version `pg_dump`/managed backup and the attachment directory at a compatible point. Keep the encryption key and secret-manager recovery procedure separately protected. Test restore into a separate database. Runtime token digests alone cannot recover lost plaintext credentials.

Before upgrading: take a tested backup, stop or drain writers, run the new `notif migrate`, and start the new binary. Migrations are serialized, versioned, checksummed and transactional. A newer database refuses an older binary. Do not edit an applied migration. Downgrades require a compatible restored database and explicit effect reconciliation.

For local verification, `python3 scripts/recovery-drill.py` creates two generated test databases in the local PostgreSQL container, backs up approved work, simulates an external effect after the snapshot, restores the snapshot, rotates credentials and confirms that the obsolete approval cannot execute again. It deletes only its generated test databases. The local timing is not a production RPO/RTO claim.

## Restoring safely

1. Keep all engines, dispatchers and runtimes disconnected from the restored database. Restore the database and attachments with the original encryption key.
2. Run `notif reconcile --restored` using the restored database configuration. It places the workspace on recovery hold, rotates every credential, marks uncertain queued/leased work and active execution intents Unknown, and supersedes old approvals/tasks. The new owner token is written with mode 0600 to `.local/recovery-owner-token`, or `NOTIF_RECOVERY_TOKEN_FILE`. Existing files are not overwritten.
3. Replace the deployment's owner secret with that new token before serving. In local `--dev`, update `.local/owner-token` securely. Old API/enrollment/worker/browser credentials must be reissued. Review provider credentials and revocations that may have changed after the backup.
4. Reconcile business effects against destination idempotency keys, resource versions and external records. Missing receipts do not prove an effect did not happen. Keep the recovery hold until that review is complete.
5. Run `notif reconcile --resume`. This allows new work; it never replays Unknown work automatically. Review individual delivery retries in Activity. An Unknown retry explicitly carries duplicate risk. Re-propose obsolete commands with current resource versions.

Recovery assumes a trusted database operator. It cannot undo an external action or recover data beyond the backup's RPO. Record production RPO, RTO and manual review time from actual drills.

## Credentials and accounts

Use different credentials for owner administration, application API access, trusted namespace-restricted launchers and each worker instance. Tokens are stored as digests; provider secrets are encrypted. Worker credentials last 24 hours, ordinary issued API/enrollment credentials 30 days and browser sessions 12 hours. Revocation/disabled accounts are checked on new requests and continuing streams.

Basic OIDC uses `NOTIF_OIDC_ISSUER`, `NOTIF_OIDC_CLIENT_ID` and `NOTIF_OIDC_CLIENT_SECRET`, with callback `/auth/oidc/callback`. The issuer must use HTTPS outside local development. Login verifies signatures, issuer/audience, expiry, nonce and PKCE. New verified subjects create a Person with no role grants. Configure IdP access appropriately; OIDC login does not import a directory or prove an organizational role. Explicit account links use immutable issuer+subject, not email matching.

Do not casually change the provider encryption key. Re-enter/rotate affected provider credentials under the new key as part of a controlled procedure. An undecryptable credential becomes a visible failed delivery rather than endlessly blocking the queue.

## Evidence and current limits

Activity shows acceptance, message/request events, worker receipts and delivery states separately. Provider acceptance and worker-reported execution remain labeled as those forms of evidence. Use stable idempotency keys across retries and process restarts. New keys create new operations.

A target expansion is limited to 1,000 recipients, a fleet status request to 100 agents, a message/answer to 32 KiB and an attachment to 5 MiB. The UI lists at most 1,000 directory entries, 200 requests/conversations/activity rows and 500 messages per opened conversation. These are bounded initial release limits, not a 10,000-worker capacity qualification. Credentials are limited to 600 API calls/minute; public login/callback paths have a bounded per-IP limiter. Failed/Unknown work is visible for recovery; retention/archival policy is an operator responsibility in this release.

`python3 scripts/quickstart-smoke.py` repeats the documented playground against a generated schema, verifies two workers and a reply, and restarts it to verify history. Port 4010 must be unused; the script refuses to stop an existing server. Run it separately from your normal playground and any database-restart drill.

## Local profiling and load qualification

`NOTIF_PROFILE_ADDR=127.0.0.1:6060` enables an optional, separate loopback diagnostics listener with `/debug/metrics` and Go `/debug/pprof/`. It is disabled by default and rejects foreign Host headers, browser Origins and non-GET requests. Never expose or proxy this listener publicly; profiles may contain application data. The metrics include server CPU, allocation/heap/peak-RSS counters and database-pool waits. See [the load harness](../tools/loadtest/README.md) and [measured results](SERVER-PERFORMANCE.md) for reproducible, isolated 25-worker tests.

The UI uses `events?stream=1&summary=1` for change notifications. This omits event data without changing visibility or cursors; integrations can continue consuming full events. Embedded UI gzip variants are built once with the UI and support conditional requests without a runtime compression pool. Provider sends use a bounded reusable connection pool and drain up to 32 ready deliveries between idle ticks. They remain serial within one engine process; a truly slow provider can still limit provider throughput. Unknown outcomes always require reconciliation before replay.
