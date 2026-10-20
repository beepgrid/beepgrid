FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --no-fund
COPY web ./
RUN npm run build

FROM golang:1.26.10-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' -o /notif ./cmd/notif
RUN mkdir /data && chown 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /notif /notif
COPY --from=builder --chown=65532:65532 /data /data
COPY LICENSE THIRD_PARTY_NOTICES.txt /licenses/
WORKDIR /data
ENV NOTIF_ADDR=0.0.0.0:4010 NOTIF_ATTACHMENTS_DIR=/data/attachments
EXPOSE 4010
ENTRYPOINT ["/notif"]
CMD ["serve"]
