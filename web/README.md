# Bonsai Web Prototype

Frontend-only Bonsai workspace prototype for validating the Railway-inspired visual direction and interaction model before backend integration.

## Run

```bash
cd web
pnpm install
pnpm dev
```

## Checks

```bash
pnpm typecheck
pnpm test
pnpm test:e2e
pnpm build
```

All data and actions in this directory are mocked in the browser. There is no API, Go bridge, WebSocket connection, database, GitHub authentication, real repository access, or real command execution.

## Structure

- `src/features/workspace` — React Flow canvas, Bonsai nodes, ELK layout
- `src/features/inspector` — selection-aware inspector
- `src/features/terminal` — resizable bottom dock and fake xterm sessions
- `src/features/command-palette` — Cmd/Ctrl+K actions
- `src/features/github` — mock pull requests
- `src/features/board` — dnd-kit project board
- `src/features/files` — mock file tree/viewer
- `src/mock` — centralized prototype data
- `src/stores` — Zustand workspace state and persistence
