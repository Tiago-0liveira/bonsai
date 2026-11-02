# Garden setup mockups

Revision 02 has no screens for Garden → Setup. These mockups fill that gap in
the same visual language: the same cell grid, palette, shell chrome, breadcrumb,
submenu, rail and footer as the [revision 02 references](../revision-02/).
The flow follows Journey A of the [original brief](../original-design-brief.md)
and the connection rules in the [Garden contract](../garden-contract.md).
They are the visual reference for [plan 04b](../../../plans/tui-redesign/04b-garden-wiring.md).

- [Offline image gallery](index.html)
- [Image manifest](manifest.json)

| ID | Screen | Backend state | Sizes |
|---|---|---|---|
| GS-01 | [Garden is not set up](png/gs-01-120x35-first-use.png) | `none` | [80×24](png/gs-01-80x24-first-use.png), 120×35, [160×48](png/gs-01-160x48-first-use.png) |
| GS-02 | [Choose the GitHub repository](png/gs-02-120x35-target.png) | `connect.AmbiguousError` | 120×35 |
| GS-03 | [Connect an existing Project](png/gs-03-120x35-connect.png) | `ConnectExisting` mode | 120×35 |
| GS-04 | [Preview the setup plan](png/gs-04-120x35-plan.png) | `connect.Plan` preview | [80×24](png/gs-04-80x24-plan.png), 120×35, [160×48](png/gs-04-160x48-plan.png) |
| GS-05 | [Confirm the setup](png/gs-05-120x35-confirm.png) | before `connect.Apply` | 120×35 |
| GS-06 | [Setup is incomplete](png/gs-06-120x35-resume.png) | `partial-setup`, `connect.Resume` | 120×35 |
| GS-07 | [Read-only connection](png/gs-07-120x35-read-only.png) | `read-only` | 120×35 |
| GS-08 | [Garden is off](png/gs-08-120x35-skipped.png) | `skipped` | 120×35 |

All repository names, logins, IDs, operation IDs and counts are made up.
Pixel sizes and the font are only for illustration; the implementation uses
terminal cells and the installed themes, including the ASCII fallback.

## Regenerate

The PNG/SVG pairs, `manifest.json`, `index.html` and `SHA256SUMS` are generated
by `generate.py`, which uses only the Python standard library. PNGs are
screenshots of the SVGs taken with a headless Chromium:

```sh
python3 -I docs/design/tui-redesign/garden-setup/generate.py \
  --png ~/.cache/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell
```

Without `--png`, only the SVGs, the manifest, the gallery and the checksums are
rewritten. The normal Go build and tests do not need any of this.
