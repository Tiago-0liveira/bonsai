# Contributing to Bonsai

Use the Go version in `go.mod`, Node 22.12.0 and pnpm 12.9.1. Ordinary Go
builds and tests require no frontend installation. Build the production UI
before running tagged embed tests or GoReleaser.

```sh
go vet ./...
go build ./...
go test ./...
go test -race ./...
pnpm --dir web install --frozen-lockfile
pnpm --dir web typecheck
pnpm --dir web lint
pnpm --dir web test
pnpm --dir web test:bundle
pnpm --dir web build
pnpm --dir web bundle:check
go test -tags embedui ./web/
pnpm --dir web exec playwright install chromium
pnpm --dir web test:e2e
```

`bonsai web` is the local product, supervised from user-level state and serving
its own UI. `bonsai __serve-dev-stack` is the hidden contributor harness: local
API, loopback webhook relay and Vite. Read [development.md](docs/development.md)
and [git-backend.md](docs/git-backend.md) before changing networking, lifecycle,
webhook or security behavior. Both modes retain exact Host/Origin checks,
capability sessions, authenticated WebSockets and mutation protections.

Keep frontend lazy loading and chunk-failure recovery intact. Bundle budgets
are enforced against historical source revisions; changes need an explicit
rationale in `web/bundle-budget.json`.

Phase 8 targets `feat/bonsai-web`. The eventual release PR from that integration
branch into `main` must carry **`release:minor`**: this release retires hosted
access and removes `bonsai serve` immediately. Release publication follows
successful native archive smoke tests; see [releasing.md](docs/releasing.md).
