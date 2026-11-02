package playgroundbackend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

// changed prepares a worktree session with a loaded Changes view.
func changed(t *testing.T, r *rig, setup func(wt string)) (string, playgroundapi.ChangesView) {
	t.Helper()
	wt := r.worktree("feature")
	setup(wt)
	r.add("s", wt)
	v, err := r.b.Changes(context.Background(), playgroundapi.ChangesRequest{SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	return wt, v
}

func diffReq(v playgroundapi.ChangesView, path string) playgroundapi.FileDiffRequest {
	return playgroundapi.FileDiffRequest{SessionID: v.SessionID, Path: path, Base: v.Base.Ref, Generation: v.Stamp.Generation}
}

func TestFileDiffHunksAndLineNumbers(t *testing.T) {
	r2 := newRig(t, "main")
	var orig strings.Builder
	for i := 1; i <= 40; i++ {
		orig.WriteString("line " + string(rune('a'+i%26)) + "\n")
	}
	write(t, filepath.Join(r2.main, "big.txt"), orig.String())
	r2.commit(r2.main, "big")
	wt := r2.worktree("feature")
	// Two edits far apart make two hunks.
	rows := strings.Split(orig.String(), "\n")
	rows[2], rows[34] = "CHANGED", "ALSO CHANGED"
	write(t, filepath.Join(wt, "big.txt"), strings.Join(rows, "\n"))
	r2.add("s", wt)
	v, err := r2.b.Changes(context.Background(), playgroundapi.ChangesRequest{SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := r2.b.FileDiff(context.Background(), diffReq(v, "big.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Hunks) != 2 || !strings.HasPrefix(d.Hunks[0].Header, "@@ ") || d.Truncated || d.Binary || d.Empty {
		t.Fatalf("hunks = %d %+v", len(d.Hunks), d)
	}
	var adds, dels int
	for _, h := range d.Hunks {
		for _, l := range h.Lines {
			switch l.Kind {
			case playgroundapi.DiffAdd:
				adds++
				if l.New == 0 || l.Old != 0 {
					t.Errorf("add numbers: %+v", l)
				}
			case playgroundapi.DiffDel:
				dels++
				if l.Old == 0 || l.New != 0 {
					t.Errorf("del numbers: %+v", l)
				}
			case playgroundapi.DiffCtx:
				if l.Old == 0 || l.New == 0 {
					t.Errorf("ctx numbers: %+v", l)
				}
			}
		}
	}
	if adds != 2 || dels != 2 || d.Head.SHA != v.Head.SHA || d.Lines == 0 {
		t.Fatalf("adds=%d dels=%d lines=%d", adds, dels, d.Lines)
	}
}

func TestFileDiffStaleAndPathValidation(t *testing.T) {
	r := newRig(t, "main")
	wt, v := changed(t, r, func(wt string) { write(t, filepath.Join(wt, "a.txt"), "a\n") })
	ctx := context.Background()

	for _, p := range []string{"", "/etc/passwd", "../base.txt", "a/../../x", `..\x`, "a\x00b"} {
		_, err := r.b.FileDiff(ctx, diffReq(v, p))
		wantCode(t, err, playgroundapi.CodeInvalidInput)
	}
	// Valid path, but not a changed file (even though it exists in the worktree).
	_, err := r.b.FileDiff(ctx, diffReq(v, "base.txt"))
	wantCode(t, err, playgroundapi.CodeFileNotChanged)
	_, err = r.b.FileDiff(ctx, diffReq(v, "missing/file.go"))
	wantCode(t, err, playgroundapi.CodeFileNotChanged)

	// A request for another generation or base is stale.
	req := diffReq(v, "a.txt")
	req.Generation++
	_, err = r.b.FileDiff(ctx, req)
	wantCode(t, err, playgroundapi.CodeStale)
	req = diffReq(v, "a.txt")
	req.Base = "somewhere-else"
	_, err = r.b.FileDiff(ctx, req)
	wantCode(t, err, playgroundapi.CodeStale)
	_, err = r.b.FileDiff(ctx, playgroundapi.FileDiffRequest{SessionID: "nobody", Path: "a.txt"})
	wantCode(t, err, playgroundapi.CodeStale)

	if _, err = r.b.FileDiff(ctx, diffReq(v, "a.txt")); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	// HEAD moves: the list is out of date.
	r.commit(wt, "moves")
	_, err = r.b.FileDiff(ctx, diffReq(v, "a.txt"))
	wantCode(t, err, playgroundapi.CodeStale)
	// A newer Changes call replaces the snapshot; the old generation is stale.
	v2, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.b.FileDiff(ctx, diffReq(v, "a.txt"))
	wantCode(t, err, playgroundapi.CodeStale)
	if _, err = r.b.FileDiff(ctx, diffReq(v2, "a.txt")); err != nil {
		t.Fatalf("new generation: %v", err)
	}
}

func TestFileDiffUntrackedBinaryRenameAndSymlink(t *testing.T) {
	r := newRig(t, "main")
	ctx := context.Background()
	_, v := changed(t, r, func(wt string) {
		write(t, filepath.Join(wt, "new.txt"), "n1\nn2\n")
		write(t, filepath.Join(wt, "bin.dat"), "a\x00b")
		if err := os.Rename(filepath.Join(wt, "base.txt"), filepath.Join(wt, "moved.txt")); err != nil {
			t.Fatal(err)
		}
		sh(t, wt, "add", "-A")
		if err := os.WriteFile(filepath.Join(wt, "moved.txt"), []byte("one\ntwo\nthree!\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/etc/passwd", filepath.Join(wt, "link")); err != nil {
			t.Skip("no symlinks")
		}
	})
	d, err := r.b.FileDiff(ctx, diffReq(v, "new.txt"))
	if err != nil || len(d.Hunks) != 1 || len(d.Hunks[0].Lines) != 2 || d.Hunks[0].Lines[0].Text != "n1" || d.Hunks[0].Lines[0].Kind != playgroundapi.DiffAdd {
		t.Fatalf("untracked = %+v err=%v", d, err)
	}
	d, err = r.b.FileDiff(ctx, diffReq(v, "bin.dat"))
	if err != nil || !d.Binary || len(d.Hunks) != 0 {
		t.Fatalf("binary = %+v err=%v", d, err)
	}
	d, err = r.b.FileDiff(ctx, diffReq(v, "moved.txt"))
	if err != nil || d.OldPath != "base.txt" || len(d.Hunks) == 0 {
		t.Fatalf("rename = %+v err=%v", d, err)
	}
	// An untracked symlink is never followed.
	d, err = r.b.FileDiff(ctx, diffReq(v, "link"))
	if err != nil || !d.Binary || len(d.Hunks) != 0 {
		t.Fatalf("symlink = %+v err=%v", d, err)
	}
}

func TestFileDiffStripsTerminalControlBytes(t *testing.T) {
	r := newRig(t, "main")
	hostile := "ok\x1b[31mred\x1b]0;title\x07\x1b[2J\r\x08tab\there \u202eevil\x00"
	_, v := changed(t, r, func(wt string) {
		write(t, filepath.Join(wt, "x.txt"), strings.ReplaceAll(hostile, "\x00", "")+"\n")
	})
	d, err := r.b.FileDiff(context.Background(), diffReq(v, "x.txt"))
	if err != nil || len(d.Hunks) != 1 {
		t.Fatalf("diff = %+v err=%v", d, err)
	}
	for _, l := range d.Hunks[0].Lines {
		for _, c := range l.Text {
			if c < 0x20 || c == 0x7f || c == 0x1b || (c >= 0x80 && c < 0xa0) || c == 0x202e {
				t.Fatalf("control rune %U survived in %q", c, l.Text)
			}
		}
		if strings.Contains(l.Text, "title") && strings.Contains(l.Text, "]0") {
			t.Fatalf("escape sequence text survived: %q", l.Text)
		}
	}
}

func TestFileDiffCaps(t *testing.T) {
	t.Run("lines", func(t *testing.T) {
		r := newRig(t, "main")
		_, v := changed(t, r, func(wt string) {
			write(t, filepath.Join(wt, "long.txt"), strings.Repeat("x\n", playgroundapi.MaxDiffLines+500))
		})
		d, err := r.b.FileDiff(context.Background(), diffReq(v, "long.txt"))
		if err != nil || !d.Truncated || d.TruncReason != playgroundapi.TruncLines || d.Lines != playgroundapi.MaxDiffLines || d.Empty {
			t.Fatalf("diff = trunc=%v reason=%q lines=%d err=%v", d.Truncated, d.TruncReason, d.Lines, err)
		}
	})
	t.Run("bytes", func(t *testing.T) {
		r := newRig(t, "main")
		_, v := changed(t, r, func(wt string) {
			write(t, filepath.Join(wt, "wide.txt"), strings.Repeat(strings.Repeat("y", 2000)+"\n", 300))
		})
		d, err := r.b.FileDiff(context.Background(), diffReq(v, "wide.txt"))
		if err != nil || !d.Truncated || d.TruncReason != playgroundapi.TruncBytes {
			t.Fatalf("diff = trunc=%v reason=%q err=%v", d.Truncated, d.TruncReason, err)
		}
		total := 0
		for _, h := range d.Hunks {
			for _, l := range h.Lines {
				total += len(l.Text)
			}
		}
		if total > playgroundapi.MaxDiffBytes {
			t.Fatalf("kept %d bytes, cap %d", total, playgroundapi.MaxDiffBytes)
		}
	})
	t.Run("tracked", func(t *testing.T) {
		r := newRig(t, "main")
		wt, _ := changed(t, r, func(wt string) {
			write(t, filepath.Join(wt, "t.txt"), strings.Repeat("z\n", playgroundapi.MaxDiffLines+10))
		})
		sh(t, wt, "add", "-A")
		v, err := r.b.Changes(context.Background(), playgroundapi.ChangesRequest{SessionID: "s"})
		if err != nil {
			t.Fatal(err)
		}
		d, err := r.b.FileDiff(context.Background(), diffReq(v, "t.txt"))
		if err != nil || !d.Truncated || d.TruncReason != playgroundapi.TruncLines {
			t.Fatalf("tracked diff = %+v err=%v", d.TruncReason, err)
		}
	})
}

func TestParsePatchAndClean(t *testing.T) {
	patch := "diff --git a/f b/f\nindex 1..2 100644\n--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@ func x()\n a\n-b\n+c\n\\ No newline at end of file\n"
	h, bin, reason, n := parsePatch(patch)
	if bin || reason != "" || len(h) != 1 || n != 4 || h[0].Header != "@@ -1,3 +1,3 @@ func x()" {
		t.Fatalf("hunks = %+v bin=%v reason=%q n=%d", h, bin, reason, n)
	}
	if l := h[0].Lines[3]; l.Kind != playgroundapi.DiffCtx || l.Old != 0 || l.New != 0 {
		t.Fatalf("no-newline marker = %+v", l)
	}
	if _, bin, _, _ := parsePatch("diff --git a/x b/x\nBinary files a/x and b/x differ\n"); !bin {
		t.Fatal("binary not detected")
	}
	if got := clean("a\x1b[1mb\x1b[0m\tc\u202e\x07", 0); got != "ab    c" {
		t.Fatalf("clean = %q", got)
	}
	if got := clean("abcdef", 4); got != "abc…" {
		t.Fatalf("clean limit = %q", got)
	}
	if got := clean("bad\xffbyte", 0); !utf8.ValidString(got) {
		t.Fatalf("clean utf8 = %q", got)
	}
}

func TestParseNumstatAndStatus(t *testing.T) {
	ns, err := parseNumstat("1\t2\ta.txt\x00-\t-\tb.bin\x003\t0\t\x00old name\x00new name\x00")
	if err != nil || len(ns) != 3 || ns[0].add != 1 || ns[0].del != 2 || !ns[1].binary || ns[2].old != "old name" || ns[2].path != "new name" {
		t.Fatalf("numstat = %+v err=%v", ns, err)
	}
	if _, err = parseNumstat("garbage\x00"); err == nil {
		t.Fatal("garbage accepted")
	}
	if _, err = parseNumstat("1\t1\t\x00only-old\x00"); err == nil {
		t.Fatal("truncated rename accepted")
	}
	k, err := parseNameStatus("M\x00a\x00A\x00b\x00R100\x00old\x00new\x00D\x00c\x00")
	if err != nil || k["a"] != 'M' || k["new"] != 'R' || k["c"] != 'D' || len(k) != 4 {
		t.Fatalf("name-status = %v err=%v", k, err)
	}
	p, err := parsePorcelain(" M a\x00?? b c\x00A  d\x00")
	if err != nil || !p.untracked["b c"] || !p.touched["a"] || !p.touched["d"] || p.untracked["a"] {
		t.Fatalf("porcelain = %+v err=%v", p, err)
	}
}
