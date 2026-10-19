# Plan 02 — Settings before feature workspaces

Status: implemented. Prerequisite: 01 — Base shell(01-base-shell.md).
Next: [03 — Accounts](03-accounts.md).

## Deliverable

The first completed workspace: Settings, with General, Appearance, Keys, Garden
and Agents categories, nested sections, explicit scope and reliable persistence.
Future integrations are honestly unavailable until their numbered plan lands.

References: [Appearance](../../design/tui-redesign/revision-02/png/s-02-120x35-appearance.png),
[compact Appearance](../../design/tui-redesign/revision-02/png/s-02-80x24-appearance.png),
[General](../../design/tui-redesign/revision-02/png/s-01-120x35-general.png),
[Keys](../../design/tui-redesign/revision-02/png/s-03-120x35-keys.png),
[Garden settings](../../design/tui-redesign/revision-02/png/s-04-120x35-garden.png).

## 02.1 — Inventory and map existing settings

- Build a scope/ownership matrix from `config.Prefs`, `.bonsai.yaml` settings,
  current preference messages and account launch defaults. Keep user preferences,
  repository config, provider defaults and local execution policies distinct.
- Personal preferences continue through core config state; repo edits continue
  through existing config writers with their comment-preserving behavior. Avoid
  a new catch-all settings file or silent writes to a repository config.
- Preserve precedence: personal override, repository value, built-in default.
  Show the effective value and its origin. Unset means inherit, not reset a
  provider to an invented default.

## 02.2 — Build categories and nested sections

- Adapt `components/prefs` and existing config-setting forms into a workspace
  body. Reuse messages and core commands rather than copying persistence logic.
- Add shell-owned breadcrumb rendering and parent navigation now that real
  submenus exist. Category, nested section and focused field need distinct
  selected styles. Escape returns one level and retains unsaved state safely.
- Appearance includes theme, neutral surface contrast, glyph fallback and density.
  Honor existing themes; gray shades must be derived or theme-defined.
- General includes editor/sort/current behavior settings and remembered workspace
  per local repository. Use canonical local repository identity; later Garden
  remote identity must not conflate local execution roots.
- Garden/Agents categories show existing settings where supported. Do not create
  functioning-looking controls for automatic review or issue setup before their
  services exist. Restore availability when those later plans land.

## 02.3 — Persistence and routing

- Add schema-compatible preference fields only as needed. Missing old fields get
  stable defaults. Preserve aliases, copy counts and unrelated configuration.
- Apply simple appearance previews immediately; cancellation or save failure
  restores the last persisted state. Show failures rather than pretending saved.
- Save through core commands and the existing atomic replacement path. If wider
  locking is needed, use the repository's locked store patterns instead of
  extending ad-hoc UI writes. Late save results cannot replace newer edits.
- Route the existing preferences binding and palette entries to Settings in
  redesign mode. Keep legacy preference overlays until final cutover.
- Key editing recognizes workspace scopes and the fixed terminal escape. Reject
  active-scope collisions while preserving intentional cross-scope reuse.

## Source seams

`internal/ui/components/prefs/prefs.go`, `internal/ui/overlays.go`,
`internal/ui/configedit.go`, `internal/ui/keys.go`, `internal/ui/theme/theme.go`,
`internal/core/config/state.go`, `internal/core/config/config.go`,
`internal/core/config/write.go` and config/key/theme tests.

## Exit criteria and verification

- [x] Settings is reachable with no agent account and no GitHub connection.
  (`TestSettingsPrefsKeyRoutesToSettings`, `TestShellInitStartsNothing`.)
- [ ] Categories and nested sections feel different from the root workspace;
      focus and Escape behavior match the design at all three sizes.
  Not proven: level, selected-style and bounded-render tests exist, but no
  interactive run or visual comparison with the mockups at 80x24, 120x35 and 160x48.
- [x] Old state/config loads, unknown unrelated values survive, and failed saves
      do not show success or overwrite newer settings.
  (`TestStateOldFileLoadsWithDefaults`, `TestUnknownKeysSurvive`,
  `TestSettingsSaveFailureRestoresLastPersisted`, `TestSettingsLateSaveResultsNeverReplaceNewerEdits`.)
- [x] Last workspace is remembered per repository without forcing Garden open.
  (`TestPersistLastWorkspace`, `TestSettingsRememberWorkspacePerRepo`, `TestSettingsMemoryOffPersistsNothing`.)
- [x] Key overrides, ASCII, mono and alternate themes work throughout the shell.
  (`TestShellHonorsKeyOverrides`, `TestShellHelpIsASCIIInASCIIMode`, preset loops in `shell_test.go` and `settings_test.go`.)
- [x] Garden/automation controls make no unsupported behavior promises.
  (`TestSettingsUnavailableFieldsChangeNothing`.)
- [ ] Targeted UI/config tests, relevant race checks and full Go checks pass.
  Not proven for the full suite: `go test ./...` has one pre-existing environment
  failure (`internal/server/localapi` `TestAgentUsageRealProviders`, which reads the
  real agent config dir and also fails on the base commit). Targeted and race checks pass.
- [x] `README.md` and `docs/tui-cli.md` explain scope, precedence and navigation.

Completion record:

- Entry: `,` (prefs binding), the Settings tab (`enter`/`↓` enters the body), and
  palette entries "Settings: General|Appearance|Keys|Garden|Agents" and
  "Settings: Edit key bindings". Settings starts no domain work.
- Code: `internal/ui/settings.go` (navigation, edits, key capture),
  `settings_registry.go` (categories, sections, scopes, Unavailable rows),
  `settings_view.go`, `keyscope.go` (scope rules and rejections), `shell.go` and
  `shell_view.go` (routing, palette, remembered workspace, hints), `overlays.go`,
  `configedit.go`, `agents.go`, `model.go`, `theme/theme.go`; `internal/core/config/state.go`
  and `errors.go` (new `prefs` fields, per-repo `workspaces`, ordered atomic writes).
- Tests: `internal/ui/settings_test.go`, `settings_keys_test.go`, `keyscope_test.go`,
  `shell_test.go`, `theme/theme_test.go`, `internal/core/config/state_test.go`.
- Checks run: gofmt (empty), `go vet ./...`, `go build ./...`, `GOOS=windows go build ./...`,
  `go test ./internal/ui/... ./internal/core/config/...` (pass), and
  `go test -race ./internal/ui/... ./internal/core/config/...` (pass).
  `go test ./...` not clean: pre-existing `TestAgentUsageRealProviders` failure, same on base commit.
  The failing test passes with `env -u CODEX_HOME go test ./internal/server/localapi -run '^TestAgentUsageRealProviders$' -count=1`.
- Demo: renders checked by tests only at the three sizes and in the presets; no
  interactive terminal session was run.
- Commit: `feat(tui): add redesign settings workspace`.
