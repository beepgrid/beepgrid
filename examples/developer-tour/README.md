# Complete developer tours

These programs demonstrate all 45 Client methods in Python, TypeScript and Go, plus advanced shared history, publication, dependency, administration and all 21 MCP tools. Use only a disposable local workspace. They create fixture agents and permissions, but do not execute business tools.

The rendered manual extracts each family region verbatim from these sources. Shared setup defines every ID, client and operation key. The advanced and MCP programs deliberately reuse the complete Python tour setup; they are not examples of shipping an operator credential to a worker.

Run the complete qualification from the repository root:

```sh
python3 scripts/documentation-check.py
python3 scripts/documentation-inventory-check.py
```

PostgreSQL must be reachable through `BEEPGRID_TEST_DATABASE_URL`, or the development default at localhost:5499. The runner generates credentials, a unique schema, loopback engine/model-fixture ports and temporary attachment storage. It runs the real engine and SDK transports, then drops only its generated schema. Output contains assertions and hashes, not tokens or conversation contents.

Coordinator qualification uses a deterministic OpenAI-compatible endpoint. It verifies configuration, structured planning, enablement, dispatch, review, summary and attribution. It does not establish DeepSeek/OpenAI model quality or a live external-channel round trip.

For manual runs, set `BEEPGRID_URL` and `BEEPGRID_TOKEN_FILE` to the selected local test workspace and configure a Coordinator provider first. Install Python from `sdk/python`, build `sdk/typescript`, or use the repository Go module. Examples use source-checkout imports until package publication.

Reference inputs live in `docs/developer/`. Source inventory changes must be reviewed along with the relevant examples. The adjacent website's `scripts/generate-developer-docs.py` generates grouped language/HTTP references from this public source, and its build validates a committed qualification snapshot without needing a cross-repository credential. Do not refresh snapshots to bypass a failed runtime check.
