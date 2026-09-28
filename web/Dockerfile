# syntax=docker/dockerfile:1

FROM node:22-alpine AS build

WORKDIR /app

RUN corepack enable && corepack prepare pnpm@10.17.1 --activate

COPY package.json ./
RUN pnpm install --no-frozen-lockfile

COPY . .

ARG VITE_BONSAI_RELAY_ORIGIN=https://api.bonsai.dev
ARG VITE_BONSAI_LOCAL_API_ORIGIN=http://127.0.0.1:7001

ENV VITE_BONSAI_RELAY_ORIGIN=${VITE_BONSAI_RELAY_ORIGIN} \
    VITE_BONSAI_LOCAL_API_ORIGIN=${VITE_BONSAI_LOCAL_API_ORIGIN}

RUN pnpm build

FROM caddy:2-alpine

ENV PORT=8080 \
    BONSAI_RELAY_ORIGIN=https://api.bonsai.dev

COPY --from=build /app/dist /srv

# Keep the Docker deployment behavior aligned with the production security
# boundary while letting the relay origin be selected at container runtime.
RUN printf '%s\n' \
    ':{$PORT:8080} {' \
    '  root * /srv' \
    '  encode zstd gzip' \
    '  header {' \
    '    Content-Security-Policy "default-src '\''self'\''; script-src '\''self'\''; style-src '\''self'\''; style-src-elem '\''self'\''; style-src-attr '\''unsafe-inline'\''; img-src '\''self'\'' data:; font-src '\''self'\''; connect-src '\''self'\'' {$BONSAI_RELAY_ORIGIN} http://127.0.0.1:7001 ws://127.0.0.1:7001; object-src '\''none'\''; frame-ancestors '\''none'\''; base-uri '\''none'\''; form-action '\''self'\'' {$BONSAI_RELAY_ORIGIN}"' \
    '    Referrer-Policy "no-referrer"' \
    '    X-Content-Type-Options "nosniff"' \
    '    X-Frame-Options "DENY"' \
    '    Permissions-Policy "loopback-network=(self), local-network=(self), local-network-access=(self)"' \
    '  }' \
    '  @assets path /assets/*' \
    '  header @assets Cache-Control "public, max-age=31536000, immutable"' \
    '  try_files {path} /index.html' \
    '  file_server' \
    '}' \
    > /etc/caddy/Caddyfile

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${PORT}/" >/dev/null || exit 1

CMD ["sh", "-ec", "if ! printf '%s' \"$BONSAI_RELAY_ORIGIN\" | grep -Eq '^https://[A-Za-z0-9.-]+(:[0-9]+)?
; then echo 'BONSAI_RELAY_ORIGIN must be an HTTPS origin such as https://relay.example.com' >&2; exit 1; fi; sed -i \"s|__BONSAI_RELAY_ORIGIN__|$BONSAI_RELAY_ORIGIN|g\" /srv/index.html; exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile"]
