#!/usr/bin/env python3
"""Exploratory Garden sketches. All drawing uses terminal cells, as in revision 02.

python3 -I generate.py [--node /path/to/node --modules /path/to/node_modules
                       --fonts /path/to/dejavu-font-directory]
Without --node, regenerate SVGs/gallery only. No application code is imported.
"""

import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import textwrap
from xml.sax.saxutils import escape

HERE = Path(__file__).resolve().parent
BG, S1, S2, S3, SEL, RULE = "#131612", "#1b1f19", "#22271f", "#2c3228", "#304324", "#41483b"
MUT, FG2, FG, HI = "#87907f", "#b6bdaf", "#d1d5db", "#e9e2d5"
GRN, LGRN, DGRN, AMB, RED, BRN = "#8db84c", "#9fc96b", "#6f8754", "#dba54c", "#d1604f", "#9c8265"
BLUE, CYAN, PLUM, CLAUDE = "#86a9b6", "#8abbb0", "#b3a0c9", "#d97757"
CW, CH, M = 11, 26, 18


def clip(text,width):
    return text if len(text)<=width else text[:width-1]+"…"


class Screen:
    def __init__(self, cols, rows, title):
        self.cols, self.rows, self.title = cols, rows, title
        self.cells = [[[" ", FG2, BG, False] for _ in range(cols)] for _ in range(rows)]
        self.hotspots = []

    def put(self, r, c, text, fg=FG2, bold=False):
        if r < 0 or r >= self.rows or c < 0 or c + len(text) > self.cols:
            raise ValueError(f"{self.title}: overflow r{r} c{c}: {text!r}")
        for i, ch in enumerate(text):
            self.cells[r][c+i][0:2] = [ch, fg]
            self.cells[r][c+i][3] = bold
        return c + len(text)

    def fill(self, r, c, w, h=1, bg=S1, clear=False):
        if min(r, c, w, h) < 0 or r+h > self.rows or c+w > self.cols:
            raise ValueError(f"{self.title}: rectangle outside grid {(r,c,w,h)}")
        for row in self.cells[r:r+h]:
            for cell in row[c:c+w]:
                cell[2] = bg
                if clear:
                    cell[0] = " "

    def right(self, r, text, fg=MUT, bold=False, pad=2):
        self.put(r, self.cols-pad-len(text), text, fg, bold)

    def line(self, r, c=2, w=None, fg=RULE):
        self.put(r, c, "─" * (w if w is not None else self.cols-c-2), fg)

    def text(self, r, c, text, w, fg=FG2, bold=False, limit=None):
        lines = textwrap.wrap(text, w, break_long_words=True, break_on_hyphens=False) or [""]
        if limit is not None:
            lines = lines[:limit]
        for i, line in enumerate(lines):
            self.put(r+i, c, line, fg, bold)
        return r + len(lines)

    def box(self, r, c, w, h, bg=S1, edge=RULE):
        self.fill(r, c, w, h, bg, True)
        self.put(r, c, "┌" + "─"*(w-2) + "┐", edge)
        self.put(r+h-1, c, "└" + "─"*(w-2) + "┘", edge)
        for rr in range(r+1, r+h-1):
            self.put(rr, c, "│", edge)
            self.put(rr, c+w-1, "│", edge)

    def button(self, r, c, label, color=GRN, primary=False, target=None):
        text = " " + label + " "
        self.fill(r, c, len(text), bg=color if primary else S3)
        self.put(r, c, text, BG if primary else color, True)
        if target:
            self.hot(r, c, len(text), 1, target, label)
        return c + len(text) + 2

    def chip(self, r, c, text, fg=GRN, bg=S2):
        self.fill(r, c, len(text)+2, bg=bg)
        self.put(r, c+1, text, fg)
        return c+len(text)+3

    def hot(self, r, c, w, h, target, label):
        self.hotspots.append(dict(row=r, col=c, w=w, h=h, target=target, label=label))

    def dim(self):
        # Terminal-compatible dimming: replace solid colors, never alpha/blur.
        for row in self.cells:
            for cell in row:
                cell[1] = "#51594b" if cell[1] != BG else BG
                cell[2] = BG if cell[2] == BG else S1
                cell[3] = False
        self.hotspots = []

    def svg(self):
        w, h = 2*M+self.cols*CW, 2*M+self.rows*CH
        out = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}" role="img" aria-labelledby="title desc">',
               f'<title id="title">{escape(self.title)} / {self.cols}×{self.rows}</title>',
               '<desc id="desc">Exploratory terminal mockup. Fictional data. All elements align to an 11 by 26 pixel terminal cell grid.</desc>',
               f'<rect width="{w}" height="{h}" fill="{BG}"/>']
        for r, row in enumerate(self.cells):
            c = 0
            while c < self.cols:
                end, bg = c+1, row[c][2]
                while end < self.cols and row[end][2] == bg:
                    end += 1
                if bg != BG:
                    out.append(f'<rect x="{M+c*CW}" y="{M+r*CH}" width="{(end-c)*CW}" height="{CH}" fill="{bg}"/>')
                c = end
        out.append('<g font-family="DejaVu Sans Mono,Menlo,monospace" font-size="18">')
        for r, row in enumerate(self.cells):
            for c, (ch, fg, _, bold) in enumerate(row):
                if ch != " ":
                    if ch in "│─┌┐└┘":
                        # Extend box-drawing strokes through each complete cell,
                        # as a terminal does, rather than leaving font line gaps.
                        x0,y0=M+c*CW,M+r*CH
                        cx,cy=x0+CW//2,y0+CH//2
                        strokes={"│":f"M{cx},{y0}V{y0+CH}","─":f"M{x0},{cy}H{x0+CW}",
                                 "┌":f"M{cx},{y0+CH}V{cy}H{x0+CW}","┐":f"M{x0},{cy}H{cx}V{y0+CH}",
                                 "└":f"M{cx},{y0}V{cy}H{x0+CW}","┘":f"M{x0},{cy}H{cx}V{y0}"}
                        out.append(f'<path d="{strokes[ch]}" stroke="{fg}" stroke-width="1" fill="none"/>')
                        continue
                    out.append(f'<text x="{M+c*CW}" y="{M+20+r*CH}" fill="{fg}" font-weight="{700 if bold else 400}">{escape(ch)}</text>')
        out += ['</g>', '</svg>']
        return "\n".join(out) + "\n"


def shell(cols=120, rows=35, title="Garden", active="Board", crumb=None, focus="Board", hints=None):
    s = Screen(cols, rows, title)
    s.fill(0, 0, cols, bg=S1)
    s.put(0, 2, "♧ BONSAI", GRN, True)
    s.right(0, "acme/my-app  ·  ✓ synced", FG2)
    s.fill(1, 0, cols, bg=S1)
    s.fill(1, 2, 10, bg=GRN)
    s.put(1, 3, "♧ Garden", BG, True)
    s.put(1, 14, "▣ Playground   ⌘ Worktrees   ◎ Accounts   ⚙ Settings", FG2)
    s.line(2)
    s.fill(3, 0, cols, bg=S2)
    s.put(3, 2, crumb or "Garden  ›  " + active, FG, True)
    s.fill(4, 0, cols, bg=S1)
    c = 2
    for name, label, target in [("Board", "▦ Board", "board"), ("Table", "▤ Table", "table"),
                                 ("Drafts", "✎ Drafts 3", "inbox"), ("Needs you", "◇ Needs you 4", "attention")]:
        selected = name == active
        s.fill(4, c, len(label)+2, bg=SEL if selected else S1)
        s.put(4, c+1, label, GRN if selected else (AMB if name in ("Drafts", "Needs you") else FG2), selected)
        s.hot(4, c, len(label)+2, 1, target, label)
        c += len(label)+3
    s.line(5)
    s.fill(rows-4, 0, cols, bg=S2)
    s.put(rows-4, 2, focus, FG, True)
    s.put(rows-3, 2, hints or "↑/↓ select  Enter open  / filter  v board/table  n new draft", GRN)
    s.put(rows-2, 2, "Tab submenu  ·  Ctrl+K commands  ·  ? help", MUT)
    s.fill(rows-1, 0, cols, bg=S1)
    s.put(rows-1, 2, "FOCUS: " + focus.split(" / ")[0], AMB, True)
    s.right(rows-1, "✓ GitHub synced 12s ago", MUT)
    return s


ITEMS = [
    dict(id=177, title="Keep account sessions when switching", typ="Feature", pri="P1", phase="Backlog", state="Approved", color=FG2, icon="○", detail="Spec s1 approved", next="Request a plan", target="approved"),
    dict(id=171, title="Explain cleanup ownership", typ="Chore", pri="P2", phase="Backlog", state="Approved", color=FG2, icon="○", detail="Spec s2 approved", next="Request a plan", target="approved"),
    dict(id=172, title="Reduce test setup time", typ="Chore", pri="P2", phase="Planning", state="Planning", color=BLUE, icon="◌", detail="Reading test helpers", next="Open planner", target="planning"),
    dict(id=148, title="Switch account after rate limit", typ="Feature", pri="P1", phase="Planning", state="Plan review", color=AMB, icon="◇", detail="Plan v3 needs approval", next="Review plan", target="plan-review"),
    dict(id=161, title="Improve process status colors", typ="Feature", pri="P2", phase="Ready", state="Ready", color=LGRN, icon="✓", detail="Plan v2 + spec s3", next="Start implementation", target="ready"),
    dict(id=151, title="Fix usage refresh after reconnect", typ="Bug", pri="P1", phase="Building", state="Running", color=CYAN, icon="▶", detail="Running account tests", next="Open session", target="running"),
    dict(id=175, title="Preserve stale PR metadata", typ="Bug", pri="P2", phase="Review", state="AI review", color=PLUM, icon="◈", detail="1 fix applied; checks run", next="Open review", target="ai-review"),
    dict(id=155, title="Fix CI flakes in worktrees", typ="Bug", pri="P1", phase="Review", state="Human review", color=AMB, icon="◇", detail="Ready for your review", next="Review changes", target="human-review"),
    dict(id=176, title="Remember worktree filters", typ="Feature", pri="P3", phase="Done", state="Done", color=DGRN, icon="✓", detail="PR #203 merged", next="View result", target="done"),
    dict(id=164, title="Recover expired provider access", typ="Bug", pri="P1", phase="Building", state="Blocked", color=RED, icon="!", detail="Sign-in needed", next="Resolve blocker", target="blocked"),
]
PHASES = [("Backlog", FG2), ("Planning", BLUE), ("Ready", LGRN), ("Building", CYAN), ("Review", PLUM), ("Done", DGRN)]


def board(cols=160, rows=48, detailed=False,selected_id=148):
    selected_item=next(it for it in ITEMS if it["id"]==selected_id)
    s = shell(cols, rows, "Garden / board", "Board", focus=f"Board / #{selected_id} / {selected_item['state']}")
    s.put(6, 2, "APPROVED WORK  10", FG, True)
    s.right(6, "Type: Any  ·  / filter", MUT)
    c = s.chip(7, 2, "● 3 active", CYAN)
    c = s.chip(7, c, "◇ 4 need you", AMB)
    c = s.chip(7, c, "! 1 blocked", RED)
    if cols >= 120:
        s.right(7, "View: " + ("Detailed 9 stages" if detailed else "Phases 6") + " ▾", FG2)
    if detailed:
        phases = [(n, next(it["color"] for it in ITEMS if it["state"] == n)) for n in
                  ["Approved", "Planning", "Plan review", "Ready", "Running", "AI review", "Human review", "Done", "Blocked"]]
    else:
        phases = PHASES
    if cols == 80:
        phases = PHASES[1:4]
    n = len(phases)
    width = (cols-4-(n-1))//n
    for i, (name, color) in enumerate(phases):
        x = 2+i*(width+1)
        w = width if i != n-1 else cols-2-x
        records = [it for it in ITEMS if (it["state"] == name if detailed else it["phase"] == name)]
        if cols == 80 and name == "Planning":
            records.sort(key=lambda it: it["id"] != 148)
        s.fill(9, x, w, bg=S3)
        s.put(9, x, "▌", color, True)
        short = {"Human review": "Human rev", "Plan review": "Plan rev"}.get(name, name) if w < 17 else name
        s.put(9, x+2, short, color, True)
        s.put(10, x+1, f"{len(records)} item" + ("s" if len(records) != 1 else ""), MUT)
        for j, item in enumerate(records):
            y = 12+j*7
            if y+6 >= rows-5:
                if y < rows-5:
                    s.put(y, x+1, f"+{len(records)-j} more", MUT)
                break
            selected = item["id"] == selected_id
            s.fill(y, x, w, 6, SEL if selected else S2)
            s.put(y, x, "▌", item["color"], True)
            s.put(y, x+1, f"#{item['id']}", FG, True)
            s.put(y, x+w-3, item["pri"], AMB if item["pri"] == "P1" else MUT)
            title_lines = textwrap.wrap(item["title"], w-2)
            for line_no, text in enumerate(title_lines[:2]):
                if line_no == 1 and len(title_lines) > 2:
                    text = text[:w-3] + "…"
                s.put(y+1+line_no,x+1,text,HI if selected else FG2)
            status = item["icon"] + " " + item["state"]
            if len(status) > w-2:
                status = item["icon"] + " " + short
            s.put(y+4, x+1, status[:w-2], item["color"], True)
            s.put(y+5,x+1,clip("Start locally" if item["target"]=="ready" else item["next"],w-2),FG2 if selected else MUT)
            s.hot(y, x, w, 6, item["target"], f"Open #{item['id']}: {item['title']}")
        for y in range(9, rows-5):
            if i:
                s.put(y, x-1, "│", RULE)
        if cols == 80 and len(records)>1:
            s.put(rows-6,x+1,"↓ 1 more item",MUT)
    if cols == 80:
        s.put(rows-5, 2, "‹ Backlog   Showing Planning → Building   Review / Done ›", MUT)
    else:
        s.put(rows-6, 2, "✎ 3 private drafts", AMB)
        s.put(rows-6, 25, "Drafts become GitHub issues only when you approve.", MUT)
    return s


def table(cols=120, rows=35, grouped=False, attention=False,selected_id=148):
    active = "Needs you" if attention else "Table"
    selected_item=next(it for it in ITEMS if it["id"]==selected_id)
    s = shell(cols, rows, "Garden / " + active, active, focus=f"{active} / #{selected_id} / {selected_item['state']}")
    title = "NEEDS YOU  4" if attention else "APPROVED WORK  10"
    s.put(6, 2, title, FG, True)
    if cols >= 120:
        s.right(6, "Group: " + ("Phase" if grouped else "None") + " ▾  / filter", MUT)
    s.put(7, 2, "Decisions, questions and blockers" if attention else "● 3 active    ◇ 4 need you    ! 1 blocked", AMB if attention else MUT)
    compact = cols == 80
    s.fill(9, 2, cols-4, bg=S3)
    if compact:
        fields = [(3, "ISSUE"), (10, "TITLE"), (44, "STATE"), (61, "NEXT")]
    else:
        fields = [(3, "ISSUE"), (11, "TITLE"), (53, "STATE"), (70, "SIGNAL"), (96, "NEXT")]
    for c, text in fields:
        s.put(9, c, text, MUT, True)
    data = [it for it in ITEMS if it["target"] in ("plan-review", "ready", "human-review", "blocked")] if attention else ITEMS
    if grouped:
        phase_order=[name for name,_ in PHASES]
        data=sorted(data,key=lambda item:phase_order.index(item["phase"]))
    y = 10
    prev = None
    for item in data:
        if grouped and prev != item["phase"]:
            s.fill(y, 2, cols-4, bg=S1)
            s.put(y, 3, "▾ " + item["phase"], next(col for name,col in PHASES if name==item["phase"]), True)
            y += 1
            prev = item["phase"]
        if y >= (rows-4 if compact else rows-7):
            break
        selected = item["id"] == selected_id
        s.fill(y, 2, cols-4, bg=SEL if selected else (S1 if y%2 == 0 else BG))
        s.put(y, 2, "▌" if selected else "│", item["color"])
        s.put(y, 3, f"#{item['id']}", FG, selected)
        s.put(y,10 if compact else 11,clip(item["title"],32 if compact else 39),HI if selected else FG2,selected)
        s.put(y, 44 if compact else 53, item["icon"]+" "+item["state"], item["color"], selected)
        if not compact:
            s.put(y,70,clip(item["detail"],24),FG2)
        next_short = {"Request a plan":"Plan", "Open planner":"Open", "Review plan":"Review", "Start implementation":"Start",
                      "Open session":"Open", "Open review":"Open", "Review changes":"Review", "View result":"View", "Resolve blocker":"Resolve"}
        s.put(y, 61 if compact else 96, next_short[item["next"]] if compact else item["next"], GRN if selected else MUT, selected)
        s.hot(y, 2, cols-4, 1, item["target"], f"Open #{item['id']}: {item['title']}")
        y += 2 if attention and not compact else 1
    if rows >= 35:
        s.line(rows-8)
        s.put(rows-7,3,f"#{selected_id}  {selected_item['detail']}",selected_item["color"])
        s.put(rows-6,3,"Changed: preserve the session; switch only after a confirmed limit." if selected_id==148 else "Next: "+selected_item["next"],FG2)
        s.button(rows-5,3,"Enter "+selected_item["next"],selected_item["color"],target=selected_item["target"])
    return s


def inbox(cols=120, rows=35):
    s = shell(cols, rows, "Garden / private drafts", "Drafts", focus="Drafts / D-014", hints="↑/↓ select  Enter open  n new draft  a archive  Esc parent")
    s.put(6, 2, "YOUR IDEAS  3", FG, True)
    s.right(6, "Private to this machine", AMB)
    s.put(7, 2, "Start with a thought. Shape it yourself, or invite an assistant.", FG2)
    compact = cols == 80
    s.fill(9, 2, cols-4, bg=S3)
    s.put(9, 3, "DRAFT", MUT)
    s.put(9, 12, "IDEA", MUT)
    s.put(9, 51 if compact else 76, "STATE", MUT)
    drafts = [("D-014", "Keep account sessions when switching", "Ready to publish", LGRN),
              ("D-013", "Search across agent sessions", "2 questions", AMB),
              ("D-012", "Explain why a worktree is locked", "Writing", MUT)]
    for i,(num,title,state,color) in enumerate(drafts):
        y = 11+i*3
        s.fill(y, 2, cols-4, 2, SEL if i==0 else S1)
        s.put(y, 2, "▌", color)
        s.put(y, 3, num, FG, i==0)
        s.put(y, 12, title[:36] if compact else title, HI if i==0 else FG2, i==0)
        s.put(y, 51 if compact else 76, state, color)
        s.put(y+1, 12, "Feature  ·  saved just now" if i==0 else "Saved yesterday  ·  no GitHub issue yet", MUT)
        s.hot(y, 2, cols-4, 2, "manual" if i!=1 else "clarify", f"Open {num}: {title}")
    if not compact:
        s.line(22)
        s.put(24, 3, "○ Describe  →  ○ Shape  →  ◇ Approve & create issue", GRN)
        s.put(26, 3, "Your draft stays here until you choose to publish it.", MUT)
        s.button(28, 3, "+ New draft", GRN, True, "capture")
    else:
        s.button(19, 3, "+ New draft", GRN, True, "capture")
    return s


def capture(cols=120,rows=35):
    s=shell(cols,rows,"Garden / new draft","Drafts","Garden  ›  Drafts  ›  New draft","Draft / Description",
            "Ctrl+S save draft  Tab action  Enter activate  Esc drafts")
    x=8 if cols>=120 else 3
    w=cols-2*x
    s.put(7,x,"WHAT'S ON YOUR MIND?",HI,True)
    s.put(9,x,"Describe a problem, an idea or something you want to change.",FG2)
    h=7 if rows>=35 else 5
    s.box(11,x,w,h,S2,GRN)
    s.text(12,x+2,"When I switch accounts, I lose the login I was already using. I'd like to move between accounts without signing in again.▏",w-4,HI)
    if rows>=35:
        s.put(20,x,"○ Write it yourself",GRN,True)
        s.put(21,x,"Shape a clear ticket using a few simple sections.",MUT)
        s.put(24,x,"○ Invite an assistant",CLAUDE,True)
        s.put(25,x,"Choose a model to help clarify the idea and ask useful questions.",MUT)
        s.button(28,x,"Continue writing →",GRN,True,"manual")
        s.button(28,x+24,"Discuss with AI…",CLAUDE,False,"assistant-options")
        s.put(29,x,"✓ Saved locally  ·  No GitHub issue yet",MUT)
    else:
        s.button(17,x,"Continue writing →",GRN,True,"manual")
        s.button(17,x+24,"Discuss with AI…",CLAUDE,False,"assistant-options")
        s.put(19,x,"✓ Saved locally  ·  No GitHub issue yet",MUT)
    return s


def draft(cols=120, rows=35, mode="manual"):
    narrow = cols == 80
    title = "Keep account sessions when switching"
    s = shell(cols, rows, "Garden / draft workspace", "Drafts", "Garden  ›  Drafts  ›  D-014", "Draft / D-014 / " + ("Discussion" if mode=="clarify" else "Specification"),
              "Tab section  Ctrl+S save draft  Ctrl+Enter review  Esc drafts")
    s.put(6, 2, "✎ " + title, HI, True)
    s.chip(7, 2, "Feature", GRN)
    s.chip(7, 13, "Private draft", AMB)
    s.right(7, "✓ saved locally", MUT)
    if narrow:
        s.fill(9, 2, 76, bg=S2)
        s.put(9, 3, "Specification", GRN, True)
        s.put(9, 22, "Discussion", MUT)
        s.put(11, 3, "PROBLEM", MUT, True)
        s.put(12, 3, "Switching accounts loses the active login.", HI)
        s.put(14, 3, "EXPECTED OUTCOME", MUT, True)
        s.put(15, 3, "Keep each account signed in when switching.", FG2)
        s.put(17, 3, "ACCEPTANCE CRITERIA", MUT, True)
        s.put(18, 3, "[ ] Switch A → B → A without signing in again.", FG2)
        s.put(19, 3, "[ ] Keep each account's configuration isolated.", FG2)
        return s
    split = (28 if cols==120 else 32) if mode=="manual" else (46 if cols==120 else 60)
    s.box(9, 2, split-3, rows-14, S1)
    s.put(10, 4, "SECTIONS" if mode=="manual" else "DISCUSSION", MUT, True)
    if mode=="manual":
        for i,name in enumerate(["Problem","Outcome","Requirements","Criteria","Questions"]):
            rr = 12+i*2
            if i==0:
                s.fill(rr,3,split-5,bg=SEL)
            s.put(rr,4,("▌ " if i==0 else "  ")+name,GRN if i==0 else FG2,i==0)
        s.put(22,4,"○ Assistant off",MUT)
        s.button(24 if rows==35 else 29, 4, "Invite assistant…", CLAUDE, target="assistant-options")
        s.text(26 if rows==35 else 33,4,"Write and approve without AI.",split-8,MUT)
    elif mode=="clarify":
        s.put(11, 4, "● Claude / personal", CLAUDE)
        s.text(13, 4, "You want account switching to preserve each existing login.", split-8, FG2)
        s.text(16, 4, "Should the active agent session keep running when you switch?", split-8, HI, True)
        y = 19
        for text,fg,bg in [("● Yes, leave it running", GRN, SEL), ("○ Stop and ask me first", FG2, S2), ("○ Write my own answer", FG2, S2)]:
            s.fill(y, 4, split-8, bg=bg)
            s.put(y, 5, text, fg)
            y += 1
        s.put(23, 4, "Notes (optional)", MUT)
        s.fill(24, 4, split-8, 2, S2)
        s.put(24, 5, "Keep the terminal attached too.▏", FG)
        s.put(27, 4, "1 of 2 useful questions  ·  Skip", MUT)
        s.button(28, 4, "Apply answer →", GRN, True, "draft-ready")
    else:
        s.put(11, 4, "✓ Two answers applied", LGRN, True)
        s.text(14, 4, "The draft now keeps the active agent running and preserves the terminal attachment.", split-8, FG2)
        s.text(19, 4, "No blocking questions remain. You can still edit every section directly.", split-8, MUT)
        s.button(25 if rows==35 else 30, 4, "Ask another question", CLAUDE, target="clarify")
    x,w = split+1, cols-split-3
    s.fill(9, x, w, bg=S3)
    s.put(9, x+1, "SPECIFICATION", FG, True)
    s.put(10, x+1, "Problem   Outcome   Criteria", MUT)
    content = [
        (12, "PROBLEM", MUT, True),
        (13, "Switching accounts loses the active login.", HI, False),
        (15, "EXPECTED OUTCOME", MUT, True),
        (16, "Keep each account signed in when switching.", FG2, False),
        (18, "REQUIREMENTS", MUT, True),
        (19, "• Preserve the current agent and terminal.", FG2, False),
        (20, "• Keep each account's config isolated.", FG2, False),
        (22, "ACCEPTANCE CRITERIA", MUT, True),
        (23, "[ ] Switch A → B → A without signing in again.", FG2, False),
        (24, "[ ] The original agent continues running.", FG2, False),
        (26, "OPEN QUESTIONS   None", LGRN, False),
    ]
    if mode=="clarify":
        content=[(r,
                  {19:"• Keep each account's config isolated.",20:"· Active-session behavior: awaiting answer.",24:"[ ] Switching preserves each stored login.",26:"OPEN QUESTIONS   1 decision to confirm"}.get(r,text),
                  AMB if r in (20,26) else fg,bold) for r,text,fg,bold in content]
    for r,text,fg,bold in content:
        s.put(r, x+1, text, fg, bold)
    if rows>35:
        s.put(29, x+1, "OUT OF SCOPE", MUT, True)
        s.put(30, x+1, "Automatic switching after rate limits.", FG2)
        s.put(33, x+1, "YOUR DECISIONS", MUT, True)
        s.put(34, x+1, "✓ Keep sessions running   ✓ Preserve attachment", GRN)
    s.put(rows-6,3,"✓ Problem stated   ✓ Criteria written   " + ("◇ 1 decision open" if mode=="clarify" else "✓ Questions resolved"),AMB if mode=="clarify" else LGRN)
    if mode=="clarify":
        s.right(rows-5,"Apply your answer before publishing.",AMB)
    else:
        s.button(rows-5, cols-40, "Review & create issue →", GRN, True, "publish")
    return s


def modal_base(target, cols=120, rows=35, source="board", title=None, color=GRN):
    selected_id=next((it["id"] for it in ITEMS if it["target"]==target),148)
    s = table(cols,rows,selected_id=selected_id) if source=="table" else board(cols,rows,selected_id=selected_id)
    s.dim()
    compact = cols == 80
    x,y,w,h = (1,3,78,rows-4) if compact else ((cols-104)//2,6,104,rows-10)
    s.box(y,x,w,h,S1,color)
    ix = x+3
    s.put(y+1, ix, title or target.upper(), FG, True)
    s.put(y+1, x+w-10, "Esc close", MUT)
    s.line(y+3, x+1, w-2)
    s.put(y+4, ix, "Overview   Specification   Plan   Activity", FG2)
    s.line(y+5, x+1, w-2)
    footer = y+h-5
    s.line(footer, x+1, w-2)
    s.put(y+h-2, ix, "Tab action  ·  Enter activate  ·  Esc restores your selection", MUT)
    s.hot(y+1,x+w-11,10,1,"@close","Close overlay and return to the same selection")
    return s, ix, y+6, w-6, footer


def ticket_modal(target, cols=120, rows=35, source="board"):
    lookup = {it["target"]:it for it in ITEMS}
    item = lookup.get(target, lookup["plan-review"])
    color = RED if target in ("stale", "blocked") else item["color"]
    s,x,y,w,f = modal_base(target,cols,rows,source,f"#{item['id']}  {item['title']}",color)
    s.put(y-4,x,item["icon"]+" "+item["state"]+"  ·  "+item["typ"]+" / "+item["pri"],color,True)
    compact = cols==80
    rail = 0 if compact else 27
    bx = x+rail
    bw = w-rail
    if rail:
        s.fill(y,x-1,24,f-y,bg=S2)
        s.put(y,x,"THIS TICKET",MUT,True)
        spec_ref="s1" if target=="approved" else "s2" if target=="planning" else "s4" if target=="stale" else "s3"
        plan_ref="Not requested" if target=="approved" else "In progress" if target=="planning" else "v2" if target in ("ready","done") else "v3"
        left = [(2,"Spec "+spec_ref,FG2),(3,"Review needed" if target=="stale" else "✓ approved by you",AMB if target=="stale" else LGRN),
                (5,"Plan "+plan_ref,FG2),(6,"Current specification" if target not in ("approved","planning","stale") else "Read-only planning" if target=="planning" else "",MUT),
                (8,"GitHub",MUT),(9,f"Issue #{item['id']} ↗",FG2)]
        for offset,text,fg in left:
            if y+offset<f:
                s.put(y+offset,x,text,fg)
    def line(offset,text,fg=FG2,bold=False):
        if y+offset < f:
            s.put(y+offset,bx,text,fg,bold)
    action, secondary, note = "Open", None, ""
    dest = None
    if target=="approved":
        line(0,"READY FOR PLANNING",FG,True)
        s.text(y+2,bx,"Switch accounts without losing an existing login or interrupting an active agent.",bw,HI)
        line(5,"✓ Specification approved · s1",LGRN)
        line(7,"Planner: Claude / personal · High",CLAUDE)
        line(8,"Reads this checkout; cannot change repository files.",MUT)
        note="Starts one read-only planning attempt. Implementation waits for you."
        action="Request a plan…"; secondary="Read specification"; dest="planning"
    elif target=="planning":
        line(0,"◌ PLANNER WORKING",BLUE,True)
        line(2,"Claude / personal  ·  High  ·  02:14 elapsed",FG)
        line(4,"✓ Read specification s2",LGRN)
        line(5,"✓ Inspect test fixtures",LGRN)
        line(6,"◌ Compare setup helpers",BLUE)
        line(8,"Latest activity: reading internal/ui test helpers",FG2)
        line(9,"Updated 8s ago  ·  read-only access",MUT)
        note="Leave this overlay and planning keeps running."
        action="Open planner"; secondary="Stop attempt…"
    elif target=="plan-review":
        line(0,"PLAN v3  /  NEEDS YOUR APPROVAL",AMB,True)
        line(2,"1. Detect a confirmed rate-limit response.",HI)
        line(3,"2. Offer another signed-in account; preserve context.",FG2)
        line(4,"3. Keep the current session until you confirm.",FG2)
        line(6,"CHANGED SINCE v2",MUT,True)
        line(7,"+ Ask before switching; retain the original session.",LGRN)
        line(8,"− Remove automatic account rotation.",RED)
        line(10,"Tests: cancellation, exhausted accounts, stale usage.",FG2)
        note="Approves plan v3 against spec s3. Coding starts only when you choose."
        action="Approve plan v3…"; secondary="Request changes…"; dest="ready"
        if not compact:
            s.chip(y+10,x,"v1  v2  [v3]",AMB)
    elif target=="ready":
        line(0,"✓ READY TO BUILD",LGRN,True)
        line(2,"Specification s3 and plan v2 are approved.",HI)
        line(4,"Implementer   Codex / work  ·  High",FG)
        line(5,"AI reviewer   Claude / review  ·  High",CLAUDE)
        line(6,"Review limit  One review + fixes + checks",FG2)
        line(8,"Worktree      bonsai/161-status-colors",FG2)
        line(9,"Permissions   Workspace write; ask for escalation",FG2)
        note="Preview the exact worktree and permissions before starting."
        action="Start implementation…"; secondary="Change run settings"
    elif target=="running":
        line(0,"▶ IMPLEMENTING THE APPROVED PLAN",CYAN,True)
        line(2,"Codex / work  ·  High  ·  06:42 elapsed",FG)
        line(4,"✓ Reconnect event handling",LGRN)
        line(5,"✓ Keep the last successful usage result",LGRN)
        line(6,"◌ Run the account reconnect tests",CYAN)
        line(8,"Latest report: go test ./internal/core/agents/...",FG2)
        line(9,"Updated 4s ago  ·  4 files changed",MUT)
        note="Next: one Claude review, fixes, checks, then your review."
        action="Open session"; secondary="Stop run…"
    elif target=="ai-review":
        line(0,"◈ AI REVIEW  /  ONE PASS",PLUM,True)
        line(2,"Claude / review  ·  High  ·  01:28 elapsed",FG)
        line(4,"✓ Review implementation against approved plan",LGRN)
        line(5,"✓ Fix one missing stale-data indicator",LGRN)
        line(6,"◌ Run checks after the fix",PLUM)
        line(8,"1 finding fixed  ·  0 left unresolved so far",FG2)
        line(9,"Final checks are still running.",MUT)
        note="Stops after this pass. You decide whether the result is acceptable."
        action="Open review activity"; secondary="Stop review…"
    elif target=="human-review":
        line(0,"◇ YOUR REVIEW",AMB,True)
        line(2,"Fixed the worktree startup race in test setup.",HI)
        line(4,"CHANGES                   VERIFICATION",MUT,True)
        line(5,"4 files  +82 / −19         ✓ Targeted tests",FG2)
        line(6,"AI review: 1 fix           ✓ Race checks",FG2)
        line(7,"Unresolved findings: 0     ✓ Build",LGRN)
        line(9,"Review fix included in checks; no second AI review.",MUT)
        line(10,"Draft PR #208  ·  not merged",FG2)
        note="Acceptance records your review. Merging remains a separate action."
        action="Open changes"; secondary="Request changes…"
    elif target=="done":
        line(0,"✓ COMPLETED",LGRN,True)
        line(2,"Worktree filters now survive switching repositories.",HI)
        line(4,"✓ Plan v2 implemented",LGRN)
        line(5,"✓ AI review + checks completed",LGRN)
        line(6,"✓ Accepted by you",LGRN)
        line(7,"✓ PR #203 merged into main",LGRN)
        line(9,"Merged today at 10:42  ·  commit 8f24c71",MUT)
        note="The worktree is retained until you choose to clean it up."
        action="Open merged PR"; secondary="Preview cleanup…"
    elif target=="blocked":
        line(0,"! BLOCKED DURING IMPLEMENTATION",RED,True)
        line(2,"The provider session needs you to sign in again.",HI)
        line(4,"YOUR WORK IS RETAINED",MUT,True)
        line(5,"Worktree: bonsai/164-provider-access",FG2)
        line(6,"3 changed files; no new run started.",FG2)
        line(8,"1. Reconnect the account in Accounts.",FG2)
        line(9,"2. Return here and explicitly retry.",FG2)
        note="Retry reuses retained work after the previous run is confirmed closed."
        action="Open account"; secondary="Inspect retained work"
    elif target=="stale":
        line(0,"! APPROVAL NEEDS ANOTHER LOOK",RED,True)
        line(2,"Specification s4 arrived after you approved plan v3.",HI)
        line(4,"Previously approved: spec s3 + plan v3",MUT)
        line(5,"Current: spec s4 · adds a cancellation requirement",AMB)
        line(7,"Review the spec changes, then revise the plan.",FG2)
        line(9,"Start implementation is unavailable.",MUT)
        note="Your old approval and plan are preserved in history."
        action="Compare specifications"; secondary="Open plan history"
    s.put(f+1,x,note[:w],MUT)
    c = s.button(f+2,x,action,color,True,dest)
    if secondary:
        s.button(f+2,c,secondary,FG2,False,"plan-feedback" if target=="plan-review" else None)
    return s


def special_modal(target,cols=120,rows=35):
    if target=="publish":
        s = draft(cols,rows,mode="ready")
        s.dim()
        x,y,w,h = 12,7,96,23
        s.box(y,x,w,h,S1,GRN)
        s.put(y+1,x+3,"◇ APPROVE SPECIFICATION & CREATE ISSUE",GRN,True)
        s.put(y+3,x+3,"Repository",MUT); s.put(y+3,x+20,"acme/my-app",FG)
        s.put(y+5,x+3,"Title",MUT); s.put(y+5,x+20,"Keep account sessions when switching",HI,True)
        s.put(y+7,x+3,"Labels",MUT)
        s.chip(y+7,x+20,"bonsai:type/feature",GRN)
        s.chip(y+7,x+42,"bonsai:stage/ready",LGRN)
        s.line(y+9,x+2,w-4)
        s.put(y+11,x+3,"✓ Publish this exact specification as a real GitHub issue.",FG2)
        s.put(y+13,x+3,"✓ Add it to this Garden's Backlog, ready for planning.",FG2)
        s.put(y+15,x+3,"Planning and implementation wait for your next action.",MUT)
        s.button(y+18,x+3,"Back to draft",FG2,target="draft-ready")
        s.button(y+18,x+23,"Approve & create issue",GRN,True,"approved")
        s.put(y+20,x+3,"Tab action  ·  Enter activate  ·  Esc back to draft",MUT)
        s.hot(y+1,x+w-10,8,1,"@close","Return to draft")
        return s
    if target=="assistant-options":
        s = draft(cols,rows)
        s.dim(); x,y,w,h=21,8,78,21
        s.box(y,x,w,h,S1,CLAUDE)
        s.put(y+1,x+3,"INVITE AN ASSISTANT",CLAUDE,True)
        s.put(y+3,x+3,"Account",MUT); s.put(y+3,x+18,"Claude / personal                  ▾",FG)
        s.put(y+5,x+3,"Model",MUT); s.put(y+5,x+18,"Profile default                    ▾",FG)
        s.put(y+7,x+3,"Effort",MUT); s.put(y+7,x+18,"High                               ▾",FG)
        s.put(y+9,x+3,"Ask about",MUT); s.put(y+9,x+18,"Only the decisions that change scope",FG2)
        s.line(y+11,x+2,w-4)
        s.put(y+12,x+3,"Shares your description, answers and working draft.",MUT)
        s.put(y+13,x+3,"Repository files are not read during drafting.",MUT)
        s.put(y+15,x+3,"Other providers appear when they support this role.",MUT)
        s.button(y+17,x+3,"Keep writing myself",FG2,target="manual")
        s.button(y+17,x+27,"Start discussion",CLAUDE,True,"clarify")
        return s
    if target=="plan-feedback":
        s,x,y,w,f = modal_base(target,title="#148  Request changes to plan v3",color=AMB)
        s.put(y-4,x,"Feedback stays attached to this exact plan revision.",MUT)
        s.put(y,x,"WHAT SHOULD CHANGE?",MUT,True)
        s.box(y+2,x,w,5,S2,AMB)
        s.put(y+3,x+2,"Keep the existing account storage format.",HI)
        s.put(y+4,x+2,"Add a test for cancelling the switch prompt.▏",HI)
        s.put(y+8,x,"Revise with Claude / personal  ·  High",CLAUDE)
        s.put(y+10,x,"The new attempt receives this feedback and plan v3.",FG2)
        s.put(f+1,x,"Starts one read-only revision attempt. Nothing starts coding.",MUT)
        s.button(f+2,x,"Back",FG2,target="plan-review")
        s.button(f+2,x+8,"Send feedback & revise plan",AMB,True,"planning")
        return s
    if target=="offline":
        s = table(); s.fill(7,2,116,bg=S2,clear=True)
        s.put(7,3,"! GitHub unavailable · showing last successful sync from 10:32",AMB)
        s.right(0,"acme/my-app  ·  ! offline",AMB)
        s.fill(28,2,116,bg=S2,clear=True)
        s.put(28,3,"Read retained work. Approval and new runs wait for a fresh connection.",MUT)
        s.button(29,3,"Retry sync",AMB)
        return s
    raise ValueError(target)


SCENES = []


def add(key,group,title,desc,note,fn,sizes=((120,35),)):
    SCENES.append(dict(key=key,group=group,title=title,description=desc,note=note,fn=fn,sizes=sizes))


add("inbox","Drafting","An inbox for thoughts, before GitHub", "Private drafts are a real destination, whether you use an assistant or write by hand.",
    "Local IDs are visibly different from GitHub issue numbers. Opening a draft preserves the list selection.",inbox,((120,35),(80,24)))
add("capture","Drafting","Start with one thought, not a form","One text field and two clear routes: keep writing yourself or discuss it with a chosen assistant.",
    "A title and type can be decided while shaping the ticket. Saving a thought is not publishing an issue.",capture,((120,35),(80,24)))
add("manual","Drafting","One draft workspace. AI is optional.","Write the problem, outcome and criteria directly. Invite an assistant only when useful.",
    "Saved locally is separate from approved and published. Wide layouts retain discussion beside the specification; 80 columns uses tabs.",lambda c,r:draft(c,r,"manual"),((120,35),(160,48),(80,24)))
add("clarify","Drafting","A question beside the draft it improves", "A short, contextual conversation with choices, your own answer and optional notes.",
    "The specification stays in view. Apply an answer deliberately; direct editing remains available.",lambda c,r:draft(c,r,"clarify"),((120,35),(160,48)))
add("draft-ready","Drafting","A readable specification, ready for your decision", "The assistant summarizes your choices; you can edit the same document before publishing.",
    "Readiness is explained with concrete criteria rather than an AI confidence score.",lambda c,r:draft(c,r,"ready"))
add("assistant-options","Drafting","A compact invitation, not a setup wizard", "Account, model and effort stay close to the drafting conversation.",
    "Role support remains explicit. This sketch does not promise unsupported provider capabilities.",lambda c,r:special_modal("assistant-options",c,r))
add("publish","Drafting","One explicit boundary to GitHub", "Review the exact repository, title, labels and consequence before creating the real issue.",
    "This proposes private local drafts before publication, a change from the current branch. Approving the spec does not start planning.",lambda c,r:special_modal("publish",c,r))
add("board","Views","Six phases, every next action visible", "Richer cards use semantic rails, readable titles and specific activity or decision labels.",
    "Click a card in the explorer to open its state-specific overlay. Blockers keep their original phase. Compact boards window columns.",board,((160,48),(120,35),(80,24)))
add("board-detailed","Views","Keep the full nine-stage view", "The detailed board preserves every stage from the original workflow.",
    "A secondary view for users who want all stages. All cards still open the same ticket overlays.",lambda c,r:board(c,r,True),((160,48),))
add("table","Views","A colorful table with an actionable signal", "Stage, latest signal and next action are separate columns over the same tickets as the board.",
    "Each row opens the same overlay as its matching board card. Narrow layouts retain the full state name and a short action.",table,((120,35),(160,48),(80,24)))
add("table-grouped","Views","A table grouped by phase", "Collapsible groups make the lifecycle scannable without horizontal board navigation.",
    "Grouping is presentation only; it does not change issue state.",lambda c,r:table(c,r,grouped=True))
add("attention","Views","One place for decisions and blockers", "Plan approval, starting ready work, human review and blocked runs share an attention view.",
    "This is a filtered view, not another state. Reading an attention item never starts a run.",lambda c,r:table(c,r,attention=True))
for key,title,desc in [
    ("approved","Approved specification → request a plan","A concise spec summary and the exact read-only planning action."),
    ("planning","Planning → supervise or leave it running","Observed activity, elapsed time and an explicit stop action."),
    ("plan-review","Plan review → understand what changed","A current candidate, revision history and a clear approval boundary."),
    ("ready","Ready → configure and preview implementation","Implementer, reviewer, review limit and permissions in one place."),
    ("running","Running → understand the current activity","Real reported steps and timestamps instead of guessed progress percentages."),
    ("ai-review","AI review → one pass with a stopping point","Review, bounded fixes and follow-up checks before handing back control."),
    ("human-review","Human review → inspect the evidence","Change summary, verification and remaining concerns before acceptance."),
    ("done","Done → retain the result and its history","Completion is tied to the merged PR; cleanup stays explicit."),
    ("blocked","Blocked → a reason and a recovery path","Retained work, the exact interruption and a deliberate return to execution."),
]:
    sizes=((120,35),(80,24)) if key in ("plan-review","running") else ((120,35),)
    add(key,"Ticket overlays",title,desc,"Solid cell backgrounds and box borders; no blur, transparency or pixel-positioned controls. Escape restores the originating selection.",lambda c,r,k=key:ticket_modal(k,c,r),sizes)
add("plan-feedback","Ticket overlays","Request changes without rebuilding the workflow","Write feedback against one revision and deliberately start the next planning attempt.",
    "The next revision remains subject to user approval.",lambda c,r:special_modal("plan-feedback"))
add("stale","Recovery","When a specification changes, explain the impact","The old approved snapshot stays visible while the new requirement blocks implementation.",
    "No automatic retargeting of approvals.",lambda c,r:ticket_modal("stale",c,r,source="table"))
add("offline","Recovery","Offline means stale data, not lost work","The last successful table stays visible, with a clear freshness boundary.",
    "No optimistic approval or run start from stale GitHub data.",lambda c,r:special_modal("offline"))


def write_gallery(manifest):
    template = (HERE/"gallery.html").read_text()
    data = json.dumps(manifest,ensure_ascii=False).replace("<","\\u003c")
    (HERE/"index.html").write_text(template.replace("/* MANIFEST */",data))


def write_sums():
    paths = [p for folder in ("svg","png") for p in (HERE/folder).glob("*") if p.is_file()]
    paths += [HERE/n for n in ("index.html","manifest.json","overview.png") if (HERE/n).exists()]
    (HERE/"SHA256SUMS").write_text("".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.relative_to(HERE)}\n" for p in sorted(paths)))


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--node")
    parser.add_argument("--modules")
    parser.add_argument("--fonts")
    args=parser.parse_args()
    for folder in ("svg","png"):
        (HERE/folder).mkdir(parents=True,exist_ok=True)
    manifest=dict(revision="garden-workflow-01",based_on="../../design/tui-redesign/revision-02",status="exploratory",cell=dict(width=CW,height=CH,margin=M),screens=[])
    for scene in SCENES:
        variants=[(c,r,"default") for c,r in scene["sizes"]]
        if scene["key"] in {it["target"] for it in ITEMS}:
            variants.append((120,35,"table"))
        for cols,rows,context in variants:
            screen=ticket_modal(scene["key"],cols,rows,source="table") if context=="table" else scene["fn"](cols,rows)
            stem=f"{scene['key']}-{cols}x{rows}"+("-table" if context=="table" else "")
            (HERE/"svg"/(stem+".svg")).write_text(screen.svg())
            entry={k:v for k,v in scene.items() if k not in ("fn","sizes")}
            entry.update(stem=stem,cols=cols,rows=rows,context=context,width=cols*CW+2*M,height=rows*CH+2*M,hotspots=screen.hotspots)
            manifest["screens"].append(entry)
    (HERE/"manifest.json").write_text(json.dumps(manifest,indent=2,ensure_ascii=False)+"\n")
    write_gallery(manifest)
    if args.node:
        cmd=[args.node,str(HERE/"render.mjs")]
        for opt in ("modules","fonts"):
            if getattr(args,opt):
                cmd += ["--"+opt,getattr(args,opt)]
        subprocess.run(cmd,check=True)
    write_sums()
    print(f"Generated {len(manifest['screens'])} cell-grid SVGs and offline gallery.")


if __name__=="__main__":
    main()
