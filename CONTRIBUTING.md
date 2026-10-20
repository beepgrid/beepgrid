# Contributing

Read docs/IMPLEMENTATION.md and AGENTS.md. Keep changes focused, add tests for observable behavior, run the verification commands, and update the capability matrix. Tests for persistence and concurrency use a real PostgreSQL database.

Do not add required enterprise modules to the core build. Do not commit provider secrets or local data. Contributions are licensed under Apache-2.0.

## Local checks

Run `docker compose up -d --wait`, then `bash scripts/verify.sh`. The script defaults to the isolated Compose PostgreSQL instance at localhost:5499. Set `NOTIF_TEST_DATABASE_URL` to a disposable PostgreSQL database for another setup; tests generate and clean their own schemas. Node 24, Go 1.26.10 (or toolchain auto-download), Python 3.10+ and psql are required. Run database restart drills separately from test suites.

`python3 scripts/recovery-drill.py` additionally requires Docker and creates/drops its own randomly named source/restore databases. Do not run it against a shared production database. `python3 scripts/openapi.py` regenerates the REST specification. Web production assets are committed so Go-only source builds contain a working application.
