#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export BEEPGRID_TEST_DATABASE_URL="${BEEPGRID_TEST_DATABASE_URL:-postgres://beepgrid:beepgrid@localhost:5499/beepgrid?sslmode=disable}"
psql "$BEEPGRID_TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -Atc 'SELECT 1' >/dev/null
npm --prefix sdk/typescript audit --package-lock-only --audit-level=moderate
npm --prefix sdk/react audit --package-lock-only --audit-level=moderate
npm --prefix workspace audit --package-lock-only --audit-level=moderate
npm --prefix sdk/typescript ci --no-fund
npm --prefix sdk/typescript test
npm --prefix tools/runtime-consumers ci --ignore-scripts --no-fund
npm --prefix tools/runtime-consumers test
python3 -m venv .local/runtime-consumer-venv
.local/runtime-consumer-venv/bin/python -m pip install -q -r tools/runtime-consumers/requirements.txt
PYTHONPATH=sdk/python .local/runtime-consumer-venv/bin/python scripts/runtime-langgraph-check.py
npm --prefix sdk/react ci --no-fund
npm --prefix sdk/react run build
npm --prefix sdk/react test
npm --prefix workspace ci --no-fund
npm --prefix workspace test
npm --prefix workspace run build
npm --prefix workspace run embed
go vet ./...
go -C sdk/go vet ./...
go -C sdk/go test -race ./... -count=1
go test -race ./... -count=1
go test -race -tags headless ./... -count=1
python3 scripts/sdk-integration.py
python3 scripts/documentation-check.py
python3 scripts/documentation-inventory-check.py
python3 -m unittest discover -s scripts -p test_codex_capacity.py
python3 scripts/release-audit.py
GOFLAGS=-buildvcs=false go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
go build -buildvcs=false -trimpath -o .local/beepgrid ./cmd/beepgrid
go build -buildvcs=false -trimpath -tags headless -o .local/beepgrid-headless ./cmd/beepgrid
python3 scripts/headless-build.py
python3 scripts/cli-smoke.py
