#!/usr/bin/env python3
"""Generate the Garden setup mockups in the revision-02 visual language.

Output matches revision-02 exports: an 11x26 px cell grid with an 18 px margin,
one <text> per glyph in DejaVu Sans Mono 18, the same palette and shell chrome.
Run from anywhere: python3 -I generate.py [--png CHROME_HEADLESS_SHELL]
"""

import hashlib
import json
import os
import subprocess
import sys
from xml.sax.saxutils import escape

HERE = os.path.dirname(os.path.abspath(__file__))

BG, S1, S2, S3, SEL, RULE = "#131612", "#1b1f19", "#22271f", "#2c3228", "#304324", "#41483b"
MUT, FG2, FG, HI = "#87907f", "#b6bdaf", "#d1d5db", "#e9e2d5"
GRN, LGRN, DGRN, AMB, RED, BRN = "#8db84c", "#9fc96b", "#6f8754", "#dba54c", "#d1604f", "#9c8265"

CW, CH, M = 11, 26, 18
FONT = 'font-family="DejaVu Sans Mono,monospace" font-size="18"'

REPO = "acme/my-app"


class Screen:
    def __init__(self, cols, rows, title):
        self.cols, self.rows, self.title = cols, rows, title
        self.cells = {}
        self.rects = []
        self.lines = []

    def put(self, row, col, text, fg=FG2, bold=False):
        if col + len(text) > self.cols - 2 or row >= self.rows:
            raise ValueError(f"{self.title}: overflow at r{row} c{col}: {text!r}")
        for i, ch in enumerate(text):
            self.cells[(row, col + i)] = (ch, fg, bold)
        return col + len(text)

    def right(self, row, text, fg=FG2, bold=False, pad=2):
        return self.put(row, self.cols - pad - len(text), text, fg, bold)

    def fill(self, row, col0, col1, color):
        """Fill cells col0..col1-1 of a row with a surface color."""
        self.rects.append((row, col0, col1, color))

    def rule(self, row, col0=2, col1=None):
        self.lines.append(("h", row, col0, self.cols - 2 if col1 is None else col1))

    def svg(self):
        w, h = 2 * M + self.cols * CW, 2 * M + self.rows * CH
        out = [
            f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}">',
            f"<title>{escape(self.title)} / {self.cols}×{self.rows}</title>",
            f'<rect width="{w}" height="{h}" fill="{BG}"/>',
        ]
        for row, c0, c1, color in self.rects:
            out.append(f'<rect x="{M + c0 * CW}" y="{M + row * CH}" width="{(c1 - c0) * CW}" height="{CH}" fill="{color}"/>')
        for _, row, c0, c1 in self.lines:
            for c in range(c0, c1):
                out.append(f'<rect x="{M + c * CW}" y="{M + row * CH + 13}" width="{CW}" height="1" fill="{RULE}"/>')
        for (row, col), (ch, fg, bold) in sorted(self.cells.items()):
            if ch == " ":
                continue
            out.append(
                f'<text x="{M + col * CW}" y="{M + 20 + row * CH}" fill="{fg}" {FONT} '
                f'font-weight="{700 if bold else 400}">{escape(ch)}</text>'
            )
        out.append("</svg>")
        return "\n".join(out) + "\n"


def chrome(s, crumbs, location, hints, focus, status, header_state=("✓", "synced", FG2), submenu_active="Setup", setup_items=True):
    """Shell chrome shared by every workspace: header, tabs, breadcrumb, submenu, footer."""
    c, r = s.cols, s.rows
    s.fill(0, 0, c, S1)
    s.put(0, 2, "♧", GRN, True)
    s.put(0, 4, "BONSAI", GRN, True)
    icon, word, color = header_state
    right = f"{REPO}  ·  {icon} {word}"
    start = c - 2 - len(right)
    s.put(0, start, f"{REPO}  ·  ", FG2)
    s.put(0, start + len(REPO) + 5, f"{icon} {word}", color)

    s.fill(1, 0, 2, S1)
    s.fill(1, 2, 12, GRN)
    s.fill(1, 12, c, S1)
    s.put(1, 3, "♧", BG, True)
    s.put(1, 5, "Garden", BG, True)
    for col, ic, name in ((14, "▣", "Playground"), (29, "⌘", "Worktrees"), (43, "◎", "Accounts"), (56, "⚙", "Settings")):
        s.put(1, col, ic, FG2)
        s.put(1, col + 2, name, FG2)
    s.rule(2)

    s.fill(3, 0, c, S2)
    col = 2
    for i, part in enumerate(crumbs):
        last = i == len(crumbs) - 1
        col = s.put(3, col, part, FG if last else MUT, last)
        if not last:
            s.put(3, col + 2, "›", BRN)
            col += 5

    s.fill(4, 0, c, S1)
    items = [("✚", "Setup")] if setup_items else []
    items += [("▦", "Board"), ("▤", "Table"), ("✎", "Drafts")]
    col = 2
    for ic, name in items:
        width = len(name) + 4
        active = name == submenu_active
        if active:
            s.fill(4, col, col + width, SEL)
        fg = GRN if active else (FG2 if not setup_items or name == "Setup" or submenu_active != "Setup" else MUT)
        s.put(4, col + 1, ic, fg, active)
        s.put(4, col + 3, name, fg, active)
        col += width + 1
    s.rule(5)

    s.fill(r - 4, 0, c, S2)
    s.put(r - 4, 2, location, FG, True)
    col = 2
    for key, desc in hints:
        col = s.put(r - 3, col, key, GRN)
        col = s.put(r - 3, col + 1, desc, GRN) + 2
    s.put(r - 2, 2, "Tab submenu  ·  Ctrl+K commands  ·  ? help", MUT)
    s.fill(r - 1, 0, c, S1)
    s.put(r - 1, 2, "FOCUS: " + focus, AMB, True)
    s.right(r - 1, status, MUT)


NOT_SET_UP = ("○", "not set up", FG2)


def button(s, row, col, text, primary=True):
    s.fill(row, col, col + len(text) + 2, S2)
    s.put(row, col + 1, text, GRN if primary else FG2, primary)


def scope_chip(s, row, text="Scope: Repository"):
    col = s.cols - 4 - len(text)
    s.fill(row, col - 1, col + len(text) + 1, S2)
    s.put(row, col, text, MUT)


def rail(s, top, bottom, width, header, choices, selected):
    for row in range(top, bottom + 1):
        s.fill(row, 2, 2 + width, S1)
    s.put(top + 1, 4, header, MUT, True)
    for i, name in enumerate(choices):
        row = top + 3 + 2 * i
        if i == selected:
            s.fill(row, 3, 2 + width - 1, SEL)
            s.put(row, 3, "▌", GRN)
            s.put(row, 5, name, FG, True)
        else:
            s.put(row, 5, name, FG2)


CHOICES = ["Create Garden", "Connect existing Project", "Skip for this repository"]


def gs01(cols, rows):
    s = Screen(cols, rows, "Garden is not set up")
    chrome(
        s, ["Garden", "Setup", "Create Garden"], "Garden / Setup / Create Garden",
        [("↑/↓", "choice"), ("Enter", "preview plan"), ("Esc", "parent")],
        "Setup / Create Garden", "Nothing changes until you confirm", NOT_SET_UP,
    )
    last = rows - 6
    s.put(6, 2, "♧", FG, True)
    s.put(6, 4, "GARDEN SETUP", FG, True)
    scope_chip(s, 6)
    if cols < 100:
        s.put(8, 3, "Garden is not set up for this repository.", HI)
        notes = ["new labels and a Project", "reuse a Project you own", "stay off; reversible"]
        for i, (name, note) in enumerate(zip(CHOICES, notes)):
            row = 10 + i
            if i == 0:
                s.fill(row, 2, cols - 2, SEL)
                s.put(row, 2, "▌", GRN)
            s.put(row, 4, name, FG if i == 0 else FG2, i == 0)
            s.put(row, 32, note, MUT)
        s.put(14, 3, f"{REPO}  ·  octo-dev  ·  Write  ·  project scope ", FG2)
        s.put(14, 56, "✓", GRN)
        s.put(15, 3, "Existing issues imported: ", FG2)
        s.put(15, 29, "0", GRN, True)
        s.put(16, 3, "Coding-agent accounts are separate (Accounts).", MUT)
        button(s, last, 3, "Preview setup plan…")
        return s

    w = 30 if cols < 140 else 34
    rail(s, 8, last, w, "SET UP", CHOICES, 0)
    x = w + 7
    v = x + 26
    s.put(9, x, "CREATE GARDEN", FG, True)
    s.put(11, x, "Garden is not set up for this repository.", HI)
    s.put(12, x, "Work is tracked as GitHub Issues you create here or import one by one.", FG2)
    fields = [
        ("Repository", [(REPO, FG, True), ("  ·  origin, upstream", MUT, False)]),
        ("GitHub identity", [("octo-dev", FG, True), ("  ·  Write access", FG2, False)]),
        ("Project scope", [("✓ project", GRN, False)]),
        ("Existing issues", [("0 imported", GRN, True), ("  ·  import one by one later", MUT, False)]),
        ("Creates", [("bonsai: labels, one Project, Stage and Type fields", FG2, False)]),
    ]
    for i, (label, parts) in enumerate(fields):
        row = 14 + 2 * i
        s.put(row, x, label, FG2)
        col = v
        for text, fg, bold in parts:
            col = s.put(row, col, text, fg, bold)
    s.put(25, x, "Your GitHub identity is not a coding-agent account. Agents live in Accounts.", MUT)
    if cols >= 140:
        s.put(26, x, "Unrelated labels, Project fields and Issue bodies are never changed.", MUT)
    button(s, last, x, "Preview setup plan…")
    return s


def gs02(cols, rows):
    s = Screen(cols, rows, "Choose the GitHub repository")
    chrome(
        s, ["Garden", "Setup", "Choose repository"], "Garden / Setup / Choose repository",
        [("↑/↓", "repository"), ("Enter", "choose"), ("Esc", "parent")],
        "Setup / Choose repository", "Credentials are removed from remote URLs", NOT_SET_UP,
    )
    s.put(6, 2, "CHOOSE THE GITHUB REPOSITORY", FG, True)
    scope_chip(s, 6)
    s.put(8, 3, "Your remotes point at different GitHub repositories. Pick the one this Garden tracks.", FG2)
    heads = [(4, "REPOSITORY"), (28, "REMOTES"), (50, "ACCESS"), (62, "KIND"), (90, "ID")]
    for col, h in heads:
        s.put(10, col, h, MUT, True)
    s.rule(11)
    data = [
        ("acme/my-app", "origin, upstream", "Write", "source", "48120394"),
        ("octo-dev/my-app", "fork", "Admin", "fork of acme/my-app", "51993011"),
    ]
    for i, rowdata in enumerate(data):
        row = 12 + 2 * i
        if i == 0:
            s.fill(row, 2, cols - 2, SEL)
            s.put(row, 2, "▌", GRN)
        for (col, _), text in zip(heads, rowdata):
            s.put(row, col, text, FG if i == 0 else FG2, i == 0 and col == 4)
    s.put(17, 3, "Remotes that resolve to the same repository ID are one choice.", MUT)
    s.put(18, 3, "The target is identified by its numeric ID; partial names never match.", MUT)
    button(s, rows - 6, 3, "Use acme/my-app ›")
    return s


def gs03(cols, rows):
    s = Screen(cols, rows, "Connect an existing Project")
    chrome(
        s, ["Garden", "Setup", "Connect existing Project"], "Garden / Setup / Connect existing Project",
        [("↑/↓", "project"), ("Enter", "select"), ("Esc", "parent")],
        "Setup / Connect existing Project", "Nothing changes until you confirm", NOT_SET_UP,
    )
    last = rows - 6
    s.put(6, 2, "♧", FG, True)
    s.put(6, 4, "GARDEN SETUP", FG, True)
    scope_chip(s, 6)
    w = 30
    rail(s, 8, last, w, "SET UP", CHOICES, 1)
    x = w + 7
    s.put(9, x, "CONNECT EXISTING PROJECT", FG, True)
    s.put(11, x, "Projects owned by acme. Pick one explicitly; fields are reused by name.", FG2)
    heads = [(x + 1, "#"), (x + 5, "PROJECT"), (x + 27, "FIELDS"), (x + 49, "STATUS")]
    for col, h in heads:
        s.put(13, col, h, MUT, True)
    s.rule(14, x, cols - 2)
    data = [
        ("4", "Roadmap", "Stage ✓  Type +", ("compatible", GRN)),
        ("7", "Bonsai Garden", "Stage ✓  Type ✓", ("! same name, unverified", AMB)),
        ("9", "Q3 Launch", "—", ("no Garden fields", MUT)),
    ]
    for i, (num, name, fields, (status, sc)) in enumerate(data):
        row = 15 + 2 * i
        if i == 0:
            s.fill(row, x, cols - 2, SEL)
            s.put(row, x, "▌", GRN)
        s.put(row, x + 1, num, FG if i == 0 else FG2, i == 0)
        s.put(row, x + 5, name, FG if i == 0 else FG2, i == 0)
        s.put(row, x + 27, fields, FG2)
        s.put(row, x + 49, status, sc)
    s.put(22, x, "A same-name Project is never adopted automatically.", MUT)
    s.put(23, x, "Choose it here only if it is yours; ownership is verified first.", MUT)
    s.put(25, x, "Missing fields are added (+). Unrelated fields are untouched.", FG2)
    button(s, last, x, "Preview setup plan…")
    return s


STEPS = [
    ("+ create", GRN, "label", "bonsai:managed", "managed marker"),
    ("+ create", GRN, "labels", "bonsai:type/feature, chore, research", "3 type labels"),
    ("= reuse", MUT, "label", "bonsai:type/bug", "existing, compatible"),
    ("+ create", GRN, "labels", "bonsai:stage/draft … done", "6 stage labels"),
    ("+ create", GRN, "Project", "acme / Bonsai Garden", "owned by acme"),
    ("+ add", GRN, "field", "Stage  (single select, 6 options)", "needs project scope"),
    ("+ add", GRN, "field", "Type  (single select, 4 options)", "needs project scope"),
    ("◇ manual", AMB, "view", "Board layout for the Project", "finish in the browser"),
]


def gs04(cols, rows):
    s = Screen(cols, rows, "Preview the setup plan")
    chrome(
        s, ["Garden", "Setup", "Preview plan"], "Garden / Setup / Preview plan",
        [("↑/↓", "step"), ("Enter", "review and apply"), ("Esc", "back")],
        "Setup / Preview plan",
        "0 issues imported / nothing written" if cols < 100 else "Preview only / nothing written", NOT_SET_UP,
    )
    last = rows - 6
    if cols < 100:
        s.put(6, 2, "SETUP PLAN", FG, True)
        scope_chip(s, 6, "Create  ·  preview only")
        s.put(8, 3, "CHANGE", MUT, True)
        s.put(8, 14, "TARGET", MUT, True)
        for i, (change, cc, _, target, _) in enumerate(STEPS):
            row = 9 + i
            if i == 0:
                s.fill(row, 2, cols - 2, SEL)
                s.put(row, 2, "▌", GRN)
            s.put(row, 3, change, cc)
            t = target if len(target) <= cols - 18 else target[: cols - 19] + "…"
            s.put(row, 14, t, FG if i == 0 else FG2, i == 0)
        button(s, last, 3, "Review and apply…")
        return s

    s.put(6, 2, "SETUP PLAN", FG, True)
    scope_chip(s, 6, "Create  ·  acme/my-app")
    s.put(8, 3, "Preview only. Nothing has been written to GitHub.", FG2)
    wide = cols >= 140
    cols_at = [(4, "#"), (8, "CHANGE"), (19, "KIND"), (29, "TARGET"), (72 if not wide else 84, "NOTE")]
    for col, h in cols_at:
        s.put(10, col, h, MUT, True)
    s.rule(11)
    for i, (change, cc, kind, target, note) in enumerate(STEPS):
        row = 12 + i
        if i == 0:
            s.fill(row, 2, cols - 2, SEL)
            s.put(row, 2, "▌", GRN)
        s.put(row, 4, str(i + 1), FG if i == 0 else MUT, i == 0)
        s.put(row, 8, change, cc, i == 0)
        s.put(row, 19, kind, FG2)
        s.put(row, 29, target, FG if i == 0 else FG2, i == 0)
        s.put(row, cols_at[4][0], note, AMB if cc == AMB else MUT)
    s.fill(21, 3, 3 + 72, S2)
    col = s.put(21, 4, "✓", GRN, True)
    s.put(21, col + 1, "0 issues imported  ·  0 issue bodies edited  ·  0 labels renamed", FG2)
    s.put(23, 3, "Project steps need the project scope; label steps still run without it.", MUT)
    s.put(24, 3, "Before each write Bonsai re-reads GitHub and reuses anything that appeared since.", MUT)
    if wide:
        s.put(25, 3, "Conflicting labels or unverified same-name Projects stop that step; nothing is overwritten.", MUT)
    button(s, last, 3, "Review and apply…")
    return s


def gs05(cols, rows):
    s = Screen(cols, rows, "Confirm the setup")
    chrome(
        s, ["Garden", "Setup", "Confirm setup"], "Garden / Setup / Confirm setup",
        [("←/→", "button"), ("Enter", "choose"), ("Esc", "cancel")],
        "Setup / Confirm setup", "Nothing written yet", NOT_SET_UP,
    )
    s.put(7, 3, "REVIEW THE CONSEQUENCE", FG, True)
    s.put(11, 3, 'Create 10 labels and the Project "Bonsai Garden" in acme, reuse 1 label,', HI)
    s.put(12, 3, "and add the Stage and Type fields to that Project.", HI)
    s.put(14, 3, "No Issue is imported, edited or relabeled. No label is renamed or deleted.", FG2)
    s.put(15, 3, "If a step fails, completed steps are kept and you can Resume.", FG2)
    s.fill(18, 2, 12, SEL)
    s.put(18, 3, "[Cancel]", GRN, True)
    s.put(18, 28, "[Apply setup]", FG2)
    s.put(20, 3, "Cancel has initial focus.", MUT)
    return s


def gs06(cols, rows):
    s = Screen(cols, rows, "Setup is incomplete")
    chrome(
        s, ["Garden", "Setup", "Incomplete setup"], "Garden / Setup / Incomplete setup",
        [("Enter", "resume"), ("↑/↓", "step"), ("Esc", "parent")],
        "Setup / Incomplete setup", "Labels created / Project pending", ("!", "setup incomplete", AMB),
    )
    s.fill(7, 3, 3 + 21, S2)
    s.put(7, 4, "! SETUP INCOMPLETE", AMB, True)
    s.put(9, 3, "Labels are ready. The Project step stopped: the GitHub token lacks the project scope.", HI)
    s.put(11, 4, "STEP", MUT, True)
    s.put(11, 46, "STATUS", MUT, True)
    s.rule(12)
    steps = [
        ("✓", GRN, "Create 10 labels", "done", GRN),
        ("✓", GRN, "Reuse label bonsai:type/bug", "done", GRN),
        ("!", RED, 'Create Project "Bonsai Garden"', "scope-missing", RED),
        ("○", MUT, "Add field Stage", "waiting", MUT),
        ("○", MUT, "Add field Type", "waiting", MUT),
        ("◇", AMB, "Manual: Project board layout", "waiting", MUT),
    ]
    for i, (ic, icc, name, status, sc) in enumerate(steps):
        row = 13 + i
        if i == 2:
            s.fill(row, 2, cols - 2, SEL)
            s.put(row, 2, "▌", GRN)
        s.put(row, 4, ic, icc, True)
        s.put(row, 6, name, FG if i == 2 else FG2, i == 2)
        s.put(row, 46, status, sc, i == 2)
    s.put(20, 3, "Grant the scope yourself, then Resume. Bonsai never refreshes or changes your login.", FG2)
    s.fill(22, 3, 3 + 30, S3)
    s.put(22, 5, "gh auth refresh -s project", FG)
    s.put(24, 3, "Operation op-7f3a  ·  completed steps are kept  ·  Resume re-reads GitHub first", MUT)
    last = rows - 6
    button(s, last, 3, "Resume setup ›")
    button(s, last, 23, "Skip Garden for now", primary=False)
    return s


def gs07(cols, rows):
    s = Screen(cols, rows, "Read-only connection")
    chrome(
        s, ["Garden", "Setup", "Read-only access"], "Garden / Setup / Read-only access",
        [("Enter", "open board"), ("Esc", "parent")],
        "Setup / Read-only access", "Read-only / no setup actions", ("◎", "read-only", FG2),
        submenu_active="Setup",
    )
    s.fill(7, 3, 3 + 25, S2)
    s.put(7, 4, "◎ READ-ONLY CONNECTION", FG, True)
    s.put(9, 3, f"You can read this Garden. Changing its setup needs Write access to {REPO}.", HI)
    fields = [
        ("Repository", [(REPO, FG, True), ("  ·  id 48120394", MUT, False)]),
        ("GitHub identity", [("octo-dev", FG, True), ("  ·  Read access", AMB, False)]),
        ("Garden", [("set up by a collaborator  ·  12 issues", FG2, False)]),
        ("Setup actions", [("unavailable: needs Write or Admin", MUT, False)]),
    ]
    for i, (label, parts) in enumerate(fields):
        row = 11 + 2 * i
        s.put(row, 3, label, FG2)
        col = 29
        for text, fg, bold in parts:
            col = s.put(row, col, text, fg, bold)
    s.put(20, 3, "Board, Table and Drafts stay readable. Approvals and launches are unavailable.", MUT)
    button(s, rows - 6, 3, "Open Board ›")
    return s


def gs08(cols, rows):
    s = Screen(cols, rows, "Garden is off for this repository")
    chrome(
        s, ["Garden", "Off"], "Garden / Off",
        [("Enter", "set up Garden"), ("Esc", "parent")],
        "Garden / Off", "Garden off for this clone", ("–", "garden off", MUT),
    )
    s.put(7, 3, "GARDEN IS OFF FOR THIS REPOSITORY", FG, True)
    s.put(9, 3, "You chose Skip. Nothing was created on GitHub and no issues are tracked.", HI)
    s.put(10, 3, "Worktrees, Playground and Accounts work normally.", FG2)
    s.put(12, 3, "This choice is stored for this local clone and can be changed at any time.", MUT)
    s.put(13, 3, "Settings › Garden shows the same state.", MUT)
    button(s, rows - 6, 3, "Set up Garden…")
    return s


SCREENS = [
    ("GS-01", "gs-01", "first-use", gs01, [(80, 24), (120, 35), (160, 48)],
     "Garden is not set up", "State none: choose Create, Connect existing or Skip; repository, identity, access and zero import are visible.",
     "Setup / Create Garden", "The rail selects the setup mode; Enter previews its plan. Nothing is written from this screen."),
    ("GS-02", "gs-02", "target", gs02, [(120, 35)],
     "Choose the GitHub repository", "Shown only when remotes resolve to different numeric repository IDs (AmbiguousError).",
     "Setup / Choose repository", "Remotes sharing an ID are one row with aliases. URLs are shown sanitized, never with credentials."),
    ("GS-03", "gs-03", "connect", gs03, [(120, 35)],
     "Connect an existing Project", "ConnectExisting: explicit Project choice; same-name Projects are flagged, never auto-adopted.",
     "Setup / Connect existing Project", "Fields are reused by name; only missing fields are added. Unrelated fields stay untouched."),
    ("GS-04", "gs-04", "plan", gs04, [(80, 24), (120, 35), (160, 48)],
     "Preview the setup plan", "Pure plan preview: every label, Project and field step, reuse and manual step; import count is zero.",
     "Setup / Preview plan", "Each row is a selectable step. Enter moves to confirmation; nothing is written here."),
    ("GS-05", "gs-05", "confirm", gs05, [(120, 35)],
     "Confirm the setup", "Exact consequence before Apply, in the C-01 confirmation style; Cancel has initial focus.",
     "Setup / Confirm setup", "Only Apply setup writes to GitHub. Escape cancels without writing."),
    ("GS-06", "gs-06", "resume", gs06, [(120, 35)],
     "Setup is incomplete", "partial-setup: completed steps kept, failed step shows a safe code, Resume re-observes first.",
     "Setup / Incomplete setup", "The user grants the scope; Bonsai never refreshes a token. Resume is the primary action."),
    ("GS-07", "gs-07", "read-only", gs07, [(120, 35)],
     "Read-only connection", "Read access: the Garden is readable, setup actions are absent with a reason.",
     "Setup / Read-only access", "No setup or approval action is offered. Board, Table and Drafts remain readable."),
    ("GS-08", "gs-08", "skipped", gs08, [(120, 35)],
     "Garden is off for this repository", "skipped: a durable, reversible opt-out for this local clone.",
     "Garden / Off", "Enter starts setup again. Settings › Garden reflects the same state."),
]


def main():
    chrome_bin = None
    if "--png" in sys.argv:
        chrome_bin = sys.argv[sys.argv.index("--png") + 1]
    os.makedirs(os.path.join(HERE, "svg"), exist_ok=True)
    os.makedirs(os.path.join(HERE, "png"), exist_ok=True)
    manifest = {"revision": "garden-setup-01", "based_on": "revision-02", "screens": []}
    for sid, prefix, slug, fn, sizes, title, desc, focus, note in SCREENS:
        for cols, rows in sizes:
            stem = f"{prefix}-{cols}x{rows}-{slug}"
            s = fn(cols, rows)
            svg_path = os.path.join(HERE, "svg", stem + ".svg")
            with open(svg_path, "w", encoding="utf-8") as f:
                f.write(s.svg())
            if chrome_bin:
                w, h = 2 * M + cols * CW, 2 * M + rows * CH
                subprocess.run(
                    [chrome_bin, "--headless", "--no-sandbox", "--hide-scrollbars",
                     f"--window-size={w},{h}", f"--screenshot={os.path.join(HERE, 'png', stem + '.png')}",
                     "file://" + svg_path],
                    check=True, capture_output=True,
                )
            manifest["screens"].append({
                "id": sid, "title": title, "workspace": "Garden", "cols": cols, "rows": rows,
                "stem": stem, "description": desc, "focus": focus, "note": note,
            })
    with open(os.path.join(HERE, "manifest.json"), "w", encoding="utf-8") as f:
        json.dump(manifest, f, indent=2, ensure_ascii=False)
        f.write("\n")
    write_gallery(manifest)
    write_sums()


def write_gallery(manifest):
    parts = [
        "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\">",
        "<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">",
        "<title>Garden setup mockups</title><style>",
        "body{background:#131612;color:#d1d5db;font-family:'DejaVu Sans Mono',monospace;margin:24px;max-width:1400px}",
        "a{color:#8db84c}h2{color:#e9e2d5;margin-top:40px}.note{color:#87907f}",
        "img{max-width:100%;height:auto;border:1px solid #41483b}figure{margin:16px 0}.sizes a{margin-right:16px}",
        "</style></head><body><h1>Garden setup / based on revision 02</h1>",
    ]
    by_id = {}
    for sc in manifest["screens"]:
        by_id.setdefault(sc["id"], []).append(sc)
    for sid, group in by_id.items():
        first = group[0]
        parts.append(f"<section id=\"{sid.lower()}\"><h2>{sid} / {escape(first['title'])}</h2>")
        parts.append(f"<p>{escape(first['description'])}</p><p class=\"note\">{escape(first['note'])}</p>")
        for sc in group:
            stem = sc["stem"]
            parts.append(
                f"<figure><figcaption>{sc['cols']} × {sc['rows']} / {escape(sc['focus'])}</figcaption>"
                f"<a href=\"png/{stem}.png\"><img src=\"png/{stem}.png\" loading=\"lazy\" "
                f"alt=\"{escape(first['title'])} at {sc['cols']} × {sc['rows']}\"></a>"
                f"<div class=\"sizes\"><a href=\"png/{stem}.png\">PNG</a><a href=\"svg/{stem}.svg\">Vector SVG</a></div></figure>"
            )
        parts.append("</section>")
    parts.append("</body></html>\n")
    with open(os.path.join(HERE, "index.html"), "w", encoding="utf-8") as f:
        f.write("\n".join(parts))


def write_sums():
    lines = []
    for root in ("png", "svg"):
        for name in sorted(os.listdir(os.path.join(HERE, root))):
            path = os.path.join(HERE, root, name)
            with open(path, "rb") as f:
                lines.append(f"{hashlib.sha256(f.read()).hexdigest()}  {root}/{name}")
    for name in ("index.html", "manifest.json"):
        with open(os.path.join(HERE, name), "rb") as f:
            lines.append(f"{hashlib.sha256(f.read()).hexdigest()}  {name}")
    with open(os.path.join(HERE, "SHA256SUMS"), "w", encoding="utf-8") as f:
        f.write("\n".join(sorted(lines, key=lambda l: l.split("  ")[1])) + "\n")


if __name__ == "__main__":
    main()
