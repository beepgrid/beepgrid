BEEPGRID_TEST_DATABASE_URL ?= postgres://beepgrid:beepgrid@localhost:5499/beepgrid?sslmode=disable
export BEEPGRID_TEST_DATABASE_URL
.PHONY: infra build build-workspace build-headless image-workspace image-headless dev playground test test-headless web workspace sdk verify clean
infra:
	docker compose up -d --wait
build: build-workspace
build-workspace:
	mkdir -p .local
	go build -buildvcs=false -trimpath -o .local/beepgrid ./cmd/beepgrid
build-headless:
	mkdir -p .local
	go build -buildvcs=false -trimpath -tags headless -o .local/beepgrid-headless ./cmd/beepgrid
image-workspace:
	docker build --target workspace -t beepgrid:workspace .
image-headless:
	docker build --target headless -t beepgrid:headless .
dev: build
	.local/beepgrid serve --dev
playground:
	python3 scripts/playground.py
test:
	go test -race ./... -count=1
test-headless:
	go test -tags headless -race ./... -count=1
web: workspace
workspace:
	npm --prefix workspace ci --no-fund
	npm --prefix workspace test
	npm --prefix workspace run build
	npm --prefix workspace run embed
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
