# Contributing

Useful contributions include a focused bug fix, a runnable runtime example, a clearer guide or a reproducible performance improvement. Describe the observable behavior, keep the change small, and include the checks that demonstrate it works.

Read the [architecture](docs/ARCHITECTURE.md) before changing engine behavior. [The documentation index](docs/README.md) points to API, SDK, integration and operational contracts. Keep the engine independent of private managed modules and third-party agent runtimes.

## Run the checks

Development requires Go 1.26.10, Node 24 as pinned in `.nvmrc`, Python 3.10+, Docker Compose and `psql`. The embedded workspace is committed intentionally so normal Go source builds need no Node installation.

```sh
docker compose up -d --wait
bash scripts/verify.sh
```

The suite defaults to PostgreSQL on localhost:5499. To use another disposable database, set `NOTIF_TEST_DATABASE_URL`. Tests create isolated schemas and remove them afterward. Never point a qualification script at production data. Run database restart drills separately from other suites.

The full suite checks race behavior in workspace and headless builds, real HTTP SDK flows, executable documentation, dependency advisories and packaging. For clean package consumers, also run `python3 scripts/sdk-package-check.py`. This packs local artifacts and installs them into temporary consumers; it does not publish them.

Frontend changes live in `workspace/`. Run its tests, build and embed commands before committing the generated `internal/web/dist/` assets. `python3 scripts/openapi.py` regenerates the REST specification. Update SDKs and executable documentation when a public contract changes.

`python3 scripts/recovery-drill.py` additionally requires Docker and creates its own source and restore databases. Use `docker compose stop` after local work to stop the database and mail services while retaining their volumes.

## Keep the boundaries clear

Use real PostgreSQL tests for persistence, concurrency, authorization and recovery. Acknowledgement confirms transport, not completion of a business action. Agent handlers own their execution checkpoints and external tool effects. People, agents and software services remain distinct identities.

Keep credentials, databases, private customer data and local measurements out of commits. Use environment variables or protected files for test credentials. Report suspected vulnerabilities through [the private reporting route](SECURITY.md), not a public issue.

Update [implementation notes](docs/IMPLEMENTATION.md) when a feature changes and keep [coverage](docs/COVERAGE.md) honest about what was tested. Contributions are licensed under Apache-2.0.
