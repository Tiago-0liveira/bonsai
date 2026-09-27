# Bonsai Web

The hosted frontend is a static Vite + React application. The public landing
route does not contact localhost; the privileged `/app` surface connects
explicitly to the loopback Bonsai API and separately consumes GitHub
invalidation events from the relay.

## Integrated development

From the repository root, prefer the hidden contributor harness:

```sh
bonsai __serve-dev-stack
```

It supervises the local API, development webhook relay, and this Vite app on
loopback. See [../docs/development.md](../docs/development.md).

For visual-only iteration:

```sh
cd web
pnpm install
pnpm dev
```

## Checks

```sh
pnpm typecheck
pnpm test
pnpm test:e2e
pnpm build
```

## Networking

Production defaults:

- local API: `http://127.0.0.1:7001`
- GitHub relay: `https://api.bonsai.dev`

The development supervisor overrides the relay to
`http://127.0.0.1:7002` and runs Vite at `http://127.0.0.1:7003`. Relay
events are invalidation signals only; canonical project state is refreshed from
the local API.

## Structure

- `src/api/localClient.ts` — loopback capability/session and WebSocket client
- `src/api/relayClient.ts` — hosted or development SSE invalidation client
- `src/features/workspace` — React Flow workspace surface
- `src/features/inspector` — selection-aware inspector
- `src/features/terminal` — terminal dock
- `src/features/github` — GitHub-facing UI
- `src/stores` — Zustand application state
