#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export NOTIF_TEST_DATABASE_URL="${NOTIF_TEST_DATABASE_URL:-postgres://notif:notif@localhost:5499/notif?sslmode=disable}"
psql "$NOTIF_TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -Atc 'SELECT 1' >/dev/null
npm --prefix sdk/typescript audit --package-lock-only --audit-level=moderate
npm --prefix sdk/react audit --package-lock-only --audit-level=moderate
npm --prefix workspace audit --package-lock-only --audit-level=moderate
npm --prefix sdk/typescript ci --no-fund
npm --prefix sdk/typescript run build
npm --prefix sdk/react ci --no-fund
npm --prefix sdk/react run build
npm --prefix sdk/react test
npm --prefix workspace ci --no-fund
npm --prefix workspace test
npm --prefix workspace run build
npm --prefix workspace run embed
go vet ./...
go test -race ./... -count=1
python3 scripts/sdk-integration.py
python3 -m unittest discover -s scripts -p test_codex_capacity.py
python3 scripts/release-audit.py
GOFLAGS=-buildvcs=false go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
go build -buildvcs=false -trimpath -o .local/notif ./cmd/notif
