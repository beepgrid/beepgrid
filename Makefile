NOTIF_TEST_DATABASE_URL ?= postgres://notif:notif@localhost:5499/notif?sslmode=disable
export NOTIF_TEST_DATABASE_URL
.PHONY: infra build dev playground test web sdk verify clean
infra:
	docker compose up -d --wait
build:
	go build -buildvcs=false -trimpath -o .local/notif ./cmd/notif
dev: build
	.local/notif serve --dev
playground:
	python3 scripts/playground.py
test:
	go test -race ./... -count=1
web:
	npm --prefix web ci --no-fund
	npm --prefix web run build
	npm --prefix web test
sdk:
	npm --prefix sdk/typescript ci --no-fund
	npm --prefix sdk/typescript run build
	npm --prefix sdk/react ci --no-fund
	npm --prefix sdk/react run build
	npm --prefix sdk/react test
	python3 scripts/sdk-integration.py
verify:
	bash scripts/verify.sh
clean:
	go clean -cache
