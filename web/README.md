# Bonsai Web

This Vite/React app ships inside release binaries and is served at the local
API's `/app`. The browser connects automatically on the same origin with an
in-memory capability and authenticated WebSocket. There is no landing page,
public hosted app, cloud fallback or OAuth control.

## Development

Use Node **22.12.0**, pnpm **12.9.1** and the Go version in `go.mod`.

```sh
pnpm --dir web install --frozen-lockfile
bonsai __serve-dev-stack
```

The hidden contributor harness supervises loopback API/webhook/Vite services on
7001/7002/7003. For visual work, `pnpm --dir web dev` runs Vite; backend work
needs an API in development mode authorizing that exact frontend origin.
The harness supplies explicit loopback `VITE_BONSAI_LOCAL_API_ORIGIN` and
`VITE_BONSAI_RELAY_ORIGIN`. Only development/test builds may use these overrides
and the SSE invalidation client. Public origins are rejected; production uses
only the page origin and backend WebSocket. See
[development.md](../docs/development.md).

## Checks and embedding

```sh
pnpm --dir web typecheck
pnpm --dir web lint
pnpm --dir web test
pnpm --dir web test:bundle
pnpm --dir web build
pnpm --dir web bundle:check
go test -tags embedui ./web/
go build -tags embedui -o bonsai .
pnpm --dir web exec playwright install chromium
pnpm --dir web test:e2e
```

Run these from the repository root. Production writes `web/dist`; the test
build writes `web/dist-e2e` and may expose test-store/profiling hooks. Embed
checks reject those hooks and retired cloud networking. Untagged builds and
ordinary Go tests require no Node and use a placeholder at `/app`.

Playwright defaults to the real Go UI fixture at `http://127.0.0.1:7011`.
Mocks use the same origin. Execution fixtures explicitly select their API on
7014; warm-start measurements keep the real fixture on 7013. No server reuses
an existing personal API. Keep those ports free.

## Loading and state

The entry keeps chunk-failure recovery eager; application, workspace, routes,
terminals and overlays load lazily. Failed imports offer an explicit reload,
without reload loops or losing the retained shell. Closed overlays and unused
terminals do not import. Activated terminals remain mounted across dock collapse
and route navigation.

Canonical state comes from epoch/sequence-ordered backend snapshots. Browser
storage contains approved preferences only. Warm-start data remains server-side;
see [performance.md](../docs/performance.md) for budgets, historical source
revisions and measurement commands.

## Appearance

Settings → Appearance offers Bonsai (the default), Modern Light, and Modern
Dark, plus any saved custom themes. Selection applies immediately across all
projects in this browser and persists under `bonsai-theme-v1`. Tabs on the same
origin synchronize through storage events; blocked storage keeps the choice in
memory for the current session.

Press Cmd-K on macOS or Ctrl-K elsewhere and choose **Change theme…** for the
nested picker. Arrow keys highlight without applying; Enter selects and closes.
Escape or Backspace with an empty theme search returns to commands; Escape at
the command root closes the menu. The theme picker also has a Back button.

Tailwind utilities consume the shared RGB tokens. Theme application remains
synchronous before the first React commit and updates terminals without
reconnecting. The CSP-compatible static xterm rules and xterm options share the
same pure color resolver, including ANSI overrides. New modern palettes keep
terminal green independent of their blue action accent. Automatic system mode
and a custom-theme editor are not included.
