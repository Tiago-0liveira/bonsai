# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.1

FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/bonsai-relay \
    ./cmd/bonsai-relay

FROM alpine:3.22

RUN apk add --no-cache ca-certificates su-exec \
    && addgroup -S bonsai \
    && adduser -S -G bonsai bonsai \
    && mkdir -p /data \
    && chown -R bonsai:bonsai /data

COPY --from=build /out/bonsai-relay /usr/local/bin/bonsai-relay

ENV PORT=8080 \
    BONSAI_RELAY_DATABASE=/data/relay.json \
    BONSAI_RELAY_ORIGIN=https://api.bonsai.dev \
    BONSAI_FRONTEND_ORIGIN=https://app.bonsai.dev

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${PORT}/healthz" >/dev/null || exit 1

# The executable reads PORT, database/origin settings, and GitHub credentials
# directly from the environment. Only the mounted data directory needs root
# setup before the process drops to the unprivileged service user.
CMD ["sh", "-ec", "db_dir=\"$(dirname \"$BONSAI_RELAY_DATABASE\")\"; mkdir -p \"$db_dir\"; chown -R bonsai:bonsai \"$db_dir\"; exec su-exec bonsai:bonsai /usr/local/bin/bonsai-relay"]
