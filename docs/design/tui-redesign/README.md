# Approved terminal redesign — revision 02

The user selected revision 02 in the design review and requested sequential
implementation plans. This directory preserves the approved visual reference;
the application has not been redesigned by this documentation change.

- [Annotated mockup PDF](revision-02/bonsai-redesign-v2.pdf)
- [Offline image gallery](revision-02/index.html)
- [Six-screen overview](revision-02/overview.png)
- [Image manifest](revision-02/manifest.json)
- [Sequential implementation plans](../../plans/tui-redesign/README.md)
- [Original design brief](original-design-brief.md)

## What is approved

1. A fully redesigned **terminal** application with five top-level workspaces:
   **Garden, Playground, Worktrees, Accounts, Settings**.
2. The clean layout gains gray/light-gray hierarchy, shaded surfaces, provider
   icons, semantic state accents and labeled usage meters. Foreground accents
   stay within the existing Bonsai semantic/provider palette; other themes and
   ASCII fallback must remain supported.
3. Garden has a selectable **Board** of lifecycle state columns and a selectable
   row **Table** over the same accepted work. **Drafts has its own submenu/inbox**;
   unvalidated ideas are not mixed with accepted work.
4. A workspace tab, raised submenu bar, breadcrumb, section rail, focused row and
   parent action make navigation depth visible. Selection and input ownership
   remain distinct.
5. Keyboard-first behavior: arrows select, Enter opens, Escape returns to the
   parent. Terminal control remains explicit and `Ctrl+]` returns input to Bonsai.
6. The implementation order is shell → Settings → Accounts → Garden → Playground
   → Worktrees, followed by final cutover. Garden itself is broken into ordered
   foundations, Board, Table, Drafts, approvals/planning and launch plans.

The original brief supplies workflow/security context. Its three-workspace
navigation sketch and implication that Worktrees would remain visually untouched
are superseded by this approved five-workspace redesign. Preserve its GitHub
Issues-first, two-approval, opt-in and local-execution constraints.

## Reference inventory

The repository contains **51 selected PNG/SVG pairs**, the annotated PDF and an
overview. Every major screen has a 120×35 reference; Board, Table, Drafts, item
detail, sessions/Observe, Accounts, Appearance and Worktrees also include 80×24
and 160×48 variants. The manifest describes each screen and its focus semantics.
The gallery links every retained size and vector source.

| IDs | Surface |
|---|---|
| G-01 / G-02 / G-03 | Approved state Board, approved Table, separate Drafts |
| G-04 / G-05 / G-06 | Item submenu, plan review, local implementation launch |
| G-07 / G-08 / G-09 / G-10 | Specification gate, planner, new Draft, type filter |
| P-01 / P-02 / P-03 / P-04 | Sessions, Observe, Control, attention filter |
| P-05 / P-06 / P-07 | Review findings, changes, retained history |
| A-01 / A-02 / A-03 | Accounts, Usage, Defaults |
| S-01 through S-05 | General, Appearance, Keys, Garden, Agents settings |
| W-01 through W-04 | Checkouts, Processes, Branches, Pull requests |
| E-01 / E-02 / E-03 / C-01 | Stale approval, offline, remote conflict, confirmation |

All Issue numbers, accounts, usage values, branch names, terminal output and
timestamps are fabricated. Rendered confirmation buttons are not evidence of a
working operation. Pixel measurements and the particular font/background are
illustrative; implementation must use terminal cell sizing and installed themes.

The larger, clickable review archive was moved out of temporary storage to
`/home/tiagoliv/bonsai-design/tui-redesign/revision-02`. It preserves all 357
selectable states, 99 canonical exports, source generators and review checks.
That machine-local archive is optional; these repository references and
the plans are self-contained. Avoid putting hundreds of near-duplicate selection
images or the 29 MB ZIP into the repository.

## Open the references

Open the PDF or `revision-02/index.html` directly. To browse the repository's
static references from Windows/WSL, run from the repository root:

```sh
python3 -m http.server 8766 --bind 127.0.0.1 --directory docs
```

Then open `http://localhost:8766/design/tui-redesign/revision-02/` in the Windows
browser. This serves documentation and design assets only. The existing review
server on port 8765 still points to the full
prototype through its compatibility path; it is not needed to read these plans.

Planning baseline: `feat/agent-status` at
`513186bf9ff8517a06a5cf064907de1dc0d01797`.
Assets are unchanged revision-02 exports; `revision-02/SHA256SUMS` records their
integrity. No generator, image package, Node tool or design server is needed for
the normal Go build/test.
