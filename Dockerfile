# syntax=docker/dockerfile:1
FROM golang:1.26.10-alpine AS go-source
WORKDIR /src
COPY go.mod go.sum go.work ./
COPY sdk/go/go.mod ./sdk/go/go.mod
# Warm external dependencies without fetching our SDK from a registry.
# COPY below restores the published go.mod; compilation uses go.work source.
RUN GOWORK=off go mod edit -replace=github.com/beepgrid/beepgrid/sdk/go=./sdk/go \
    && GOWORK=off go mod download
COPY . .
RUN mkdir /data && chown 65532:65532 /data

# This branch has no dependency on the Node/web build stage.
FROM go-source AS headless-builder
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -tags headless -ldflags='-s -w' -o /notif ./cmd/notif

FROM node:24.22.1-alpine AS web
WORKDIR /src/workspace
COPY workspace/package*.json ./
RUN npm ci --no-fund
COPY workspace ./
COPY sdk/typescript/src/runtime/catalog.ts ../sdk/typescript/src/runtime/catalog.ts
COPY internal/web/web.go ../internal/web/web.go
RUN npm run build && npm run embed

FROM go-source AS workspace-builder
COPY --from=web /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' -o /notif ./cmd/notif

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99 AS runtime-base
USER 65532:65532
COPY --from=go-source --chown=65532:65532 /data /data
COPY LICENSE THIRD_PARTY_NOTICES.txt /licenses/
WORKDIR /data
ENV NOTIF_ADDR=0.0.0.0:4010 NOTIF_ATTACHMENTS_DIR=/data/attachments
EXPOSE 4010
ENTRYPOINT ["/notif"]
CMD ["serve"]

FROM runtime-base AS headless
COPY --from=headless-builder /notif /notif

# The default image retains the existing all-in-one experience.
FROM runtime-base AS workspace
COPY --from=workspace-builder /notif /notif
