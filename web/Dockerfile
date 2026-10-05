# syntax=docker/dockerfile:1

FROM node:22-alpine AS build

WORKDIR /app

RUN corepack enable && corepack prepare pnpm@12.9.1 --activate

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

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
COPY docker-entrypoint.sh /usr/local/bin/bonsai-web-entrypoint
RUN chmod +x /usr/local/bin/bonsai-web-entrypoint

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

ENTRYPOINT ["/usr/local/bin/bonsai-web-entrypoint"]
