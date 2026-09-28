# Contributing to Bonsai

Build and test the Go code with the repository Go version, and use `pnpm` in
`web/` for the frontend checks.

The public product path and the contributor development harness are deliberately
different:

- `bonsai serve` starts only the secured loopback Bonsai API and is intended to
  be used with `https://app.bonsai.dev/app`.
- `bonsai __serve-dev-stack` is an internal integration harness that starts the
  local API, development webhook relay, and local Vite frontend on loopback.
  It is intentionally hidden from `bonsai help` and is not a supported
  production deployment mode.

Read [docs/development.md](docs/development.md) before changing serve, webhook,
browser-origin, relay, or frontend networking code. The development stack must
exercise the same local security boundary rather than disabling it.
