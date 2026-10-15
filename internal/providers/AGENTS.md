# internal/providers guide

One package per provider (`claude`, `codex`, `antigravity`) implementing `agents.Provider` (`internal/core/agents/provider.go`): `ID`, `Capabilities`, `SetupAccount`, `PrepareSession`, `FinalizeSession`, `Usage`. Optional capabilities are separate interfaces found by type assertion (`Describer`, `LaunchValidator`/`LaunchSchema`, `ModelLister`, `SetupSchema`, `AccountAuthenticator`, `AccountRemover`, `ProfileEditor`, `UsagePolicy`, `UsageLayout`, `UsageNoter`, ...). Assert them at compile time in `provider.go`: `var _ agents.ModelLister = (*Provider)(nil)`.

## Wiring a provider

1. New package `internal/providers/<id>` with `New(accounts agents.AccountStore, sessions agents.SessionStore) *Provider`.
2. Register in `internal/agentruntime/runtime.go` (`registry.Register(...)`). Add to `agents.KnownProviders` in `internal/core/agents/registry.go` so launch UIs list it even when unavailable.
3. Typical files (mirror claude/codex): `provider.go`, `binary.go` (resolve CLI), `paths.go`, `environment.go` (scrub inherited env, set config dir), `profile.go`/`settings.go`, `setup.go` + `setup_spec.go` (form schema), `launch.go`, `usage.go` + `usage_headline.go`, `models.go`, `status.go`, `seed.go`, `reauth`, `remove.go`.
4. Web side: `web/src/api/agents.ts`, `web/src/features/agents`, `web/src/mock/agentProviders.ts`, provider marks in e2e (`provider-marks.spec.ts`).

## Invariants

- Per-profile isolated config dir under bonsai's data root (`agents.DefaultAgentsDataRoot`): `CLAUDE_CONFIG_DIR`, `CODEX_HOME`. Scrub inherited provider vars (`CLAUDE*`, `ANTHROPIC_*`, `CODEX_*`, `OPENAI_*`; only `CODEX_CA_CERTIFICATE` kept). Do not override `HOME`.
- Never write the user's real `~/.claude`, `~/.claude.json`, `~/.codex`. Seeding reads them and copies only an allowlist (no credentials, history, sessions). Probe/setup runs get a throwaway or bonsai-owned dir.
- Never refresh a provider login or OAuth token. Read stored tokens only; the provider CLI refreshes. Antigravity usage reads the access token and falls back to running `agy`; credentials reconcile back to the vault.
- Setup talks through `agents.SetupIO` only (no process stdio). Secrets are never logged, echoed or put in argv/env of long-lived processes.
- Usage is best-effort: never error the whole dashboard for one account; cache (`usage_cache.go`, 1 min TTL for claude/codex); undocumented endpoints (Cloud Code, app-server) may change, so parse defensively and test against `testdata/` recordings.
- Tests use fake CLIs from `testdata/` (`fakeclaude.sh`, `fakecodex.sh`) put first on `PATH` with an empty temp `HOME`. They are POSIX-only: skip on Windows. Run `go test ./internal/providers/<id>/... ./internal/core/agents/... ./internal/core/agentterminal/...` and `./internal/cli/...` (golden output) after changes.
