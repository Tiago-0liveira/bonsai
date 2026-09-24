package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
)

// cmdSpawn starts a background process managed by the daemon (auto-starting it).
func cmdSpawn(repoDir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("spawn", flag.ContinueOnError)
	fs.SetOutput(out)
	wt := fs.String("wt", "", "worktree to run in (branch or path; default: current dir)")
	label := fs.String("label", "", "short display name")
	restart := fs.String("restart", "", "restart policy: no | on-failure | always")
	if err := fs.Parse(args); err != nil {
		return err
	}
	command := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("usage: bonsai spawn [--wt X] [--label L] [--restart MODE] <command...>")
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if *wt != "" {
		if dir, err = resolveWorktree(repoDir, *wt); err != nil {
			return err
		}
	}
	lbl := *label
	if lbl == "" {
		lbl = command
	}
	var policy *procstore.Policy
	if *restart != "" {
		if !procstore.ValidMode(*restart) {
			return fmt.Errorf("invalid --restart %q (no|on-failure|always)", *restart)
		}
		policy = &procstore.Policy{Mode: *restart, MaxRestarts: procstore.DefaultPolicy().MaxRestarts}
	}

	rec, err := client.For(repoDir).Spawn(dir, "", lbl, command, policy)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "started #%d (pid %d)\n", rec.ID, rec.PID)
	return nil
}

// cmdPS lists background processes for this repo, or every repo with --all.
func cmdPS(repoDir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ps", flag.ContinueOnError)
	fs.SetOutput(out)
	all := fs.Bool("all", false, "list processes across every repo")
	wt := fs.String("wt", "", "restrict to a worktree (branch or path)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	if *all {
		fmt.Fprintln(tw, "REPO\tID\tWT\tNAME\tPID\tSTATUS\tUP\tURL")
		daemons, err := procstore.ListDaemons()
		if err != nil {
			return err
		}
		sort.Slice(daemons, func(i, j int) bool { return daemons[i].Root < daemons[j].Root })
		for _, d := range daemons {
			c := client.For(d.Root)
			recs, err := c.List()
			if err != nil {
				continue
			}
			for _, r := range recs {
				printPSRow(tw, c, r, filepath.Base(d.Root))
			}
		}
		return tw.Flush()
	}

	c := client.For(repoDir)
	recs, err := c.List()
	if err != nil {
		return err
	}
	var wtPath string
	if *wt != "" {
		if wtPath, err = resolveWorktree(repoDir, *wt); err != nil {
			return err
		}
	}
	fmt.Fprintln(tw, "ID\tWT\tNAME\tPID\tSTATUS\tUP\tURL")
	for _, r := range recs {
		if wtPath != "" && filepath.Clean(r.Worktree) != filepath.Clean(wtPath) {
			continue
		}
		printPSRow(tw, c, r, "")
	}
	return tw.Flush()
}

func printPSRow(tw *tabwriter.Writer, c *client.Client, r *procstore.Record, repo string) {
	url := coreexec.LastLocalURL(readTail(c.Store().LogPath(r.ID), 64<<10))
	up := ""
	if r.Status == procstore.StatusRunning {
		up = humanDuration(time.Since(r.StartedAt))
	}
	wt := filepath.Base(r.Worktree)
	name := r.Label
	if name == "" {
		name = r.Command
	}
	if repo != "" {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%d\t%s\t%s\t%s\n", repo, r.ID, wt, name, r.PID, r.Status, up, url)
		return
	}
	fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%s\t%s\t%s\n", r.ID, wt, name, r.PID, r.Status, up, url)
}

// cmdLogs prints (and optionally follows) one process's log.
func cmdLogs(repoDir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(out)
	follow := fs.Bool("f", false, "follow the log")
	n := fs.Int("n", 0, "show only the last N lines")
	grep := fs.String("grep", "", "show only lines matching PATTERN")
	insensitive := fs.Bool("i", false, "case-insensitive grep")

	// Separate flags from positional id argument so `bonsai logs <id> -n 100`
	// and `bonsai logs -n 100 <id>` both parse correctly.
	var flagArgs []string
	var idStr string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			if (arg == "-n" || arg == "--n" || arg == "-grep" || arg == "--grep") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flagArgs = append(flagArgs, args[i])
			}
		} else if idStr == "" {
			idStr = arg
		} else {
			flagArgs = append(flagArgs, arg)
		}
	}

	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if idStr == "" && fs.NArg() > 0 {
		idStr = fs.Arg(0)
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || idStr == "" {
		return fmt.Errorf("usage: bonsai logs <id> [-f] [-n N] [--grep P] [-i]")
	}
	c := client.For(repoDir)
	// The log carries bonsai's own run delimiters in an encoded form; render them
	// as readable rules on the way out (see procstore.MarkerRenderer).
	mr := &procstore.MarkerRenderer{}
	err = c.Logs(id, *follow, *n, *grep, *insensitive, func(chunk string) error {
		_, werr := io.WriteString(out, mr.Render(chunk))
		return werr
	})
	if rest := mr.Flush(); rest != "" {
		_, _ = io.WriteString(out, rest)
	}
	return err
}

// cmdGrep searches one process's log, or every process's log when no id is given.
func cmdGrep(repoDir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("grep", flag.ContinueOnError)
	fs.SetOutput(out)
	all := fs.Bool("all", false, "search across every repo")
	insensitive := fs.Bool("i", false, "case-insensitive")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pattern := fs.Arg(0)
	if pattern == "" {
		return fmt.Errorf("usage: bonsai grep [-i] [--all] <pattern> [id]")
	}

	search := func(c *client.Client, prefix string) {
		recs, err := c.List()
		if err != nil {
			return
		}
		for _, r := range recs {
			mr := &procstore.MarkerRenderer{}
			_ = c.Logs(r.ID, false, 0, pattern, *insensitive, func(chunk string) error {
				return writePrefixed(out, fmt.Sprintf("%s%d", prefix, r.ID), mr.Render(chunk))
			})
		}
	}

	if *all {
		daemons, _ := procstore.ListDaemons()
		for _, d := range daemons {
			search(client.For(d.Root), filepath.Base(d.Root)+":")
		}
		// also idle repos are missed here; --all favors live daemons.
		return nil
	}

	c := client.For(repoDir)
	if id, err := strconv.Atoi(fs.Arg(1)); err == nil {
		mr := &procstore.MarkerRenderer{}
		return c.Logs(id, false, 0, pattern, *insensitive, func(chunk string) error {
			_, werr := io.WriteString(out, mr.Render(chunk))
			return werr
		})
	}
	search(c, "")
	return nil
}

// cmdKill stops a process (id), all processes (--all), or a worktree's (--wt).
// Process ids are local to each repo's daemon, so an id from another repo
// (e.g. spotted via `bonsai ps --all`) is invisible to this repo's daemon by
// default; --repo targets that other repo explicitly.
func cmdKill(repoDir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("kill", flag.ContinueOnError)
	fs.SetOutput(out)
	all := fs.Bool("all", false, "kill every process in the target repo")
	wt := fs.String("wt", "", "kill a worktree's processes")
	repo := fs.String("repo", "", "target another repo by root path or directory name (see 'bonsai ps --all')")
	if err := fs.Parse(args); err != nil {
		return err
	}

	targetDir := repoDir
	if *repo != "" {
		root, err := resolveDaemonRepo(*repo)
		if err != nil {
			return err
		}
		targetDir = root
	}
	c := client.For(targetDir)

	var wtPath string
	if *wt != "" {
		var err error
		if wtPath, err = resolveWorktree(targetDir, *wt); err != nil {
			return err
		}
	}
	id := 0
	if !*all && wtPath == "" {
		var err error
		if id, err = strconv.Atoi(fs.Arg(0)); err != nil {
			return fmt.Errorf("usage: bonsai kill [--repo R] <id | --all | --wt X>")
		}
	}
	killed, err := c.Kill(id, *all, wtPath)
	if err != nil {
		return err
	}
	if len(killed) == 0 {
		if *repo == "" && id > 0 {
			fmt.Fprintln(out, "no matching running processes (id belongs to another repo? pass --repo, see 'bonsai ps --all')")
			return nil
		}
		fmt.Fprintln(out, "no matching running processes")
		return nil
	}
	fmt.Fprintf(out, "killed %v\n", killed)
	return nil
}

// resolveDaemonRepo finds a live daemon's repo root from ref, matched by exact
// root path or by directory basename (see `bonsai ps --all`'s REPO column).
func resolveDaemonRepo(ref string) (string, error) {
	daemons, err := procstore.ListDaemons()
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(ref)
	var matches []string
	for _, d := range daemons {
		if d.Root == clean {
			return d.Root, nil
		}
		if filepath.Base(d.Root) == ref {
			matches = append(matches, d.Root)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no live daemon matches --repo %q (see 'bonsai ps --all')", ref)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("--repo %q matches multiple repos: %v (use the full path)", ref, matches)
	}
}

// cmdRestart restarts a process by id.
func cmdRestart(repoDir string, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: bonsai restart <id>")
	}
	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("usage: bonsai restart <id>")
	}
	rec, err := client.For(repoDir).Restart(id)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "restarted #%d (pid %d)\n", rec.ID, rec.PID)
	return nil
}

// cmdAttach streams a process's log to the terminal (Ctrl-C detaches).
func cmdAttach(repoDir string, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: bonsai attach <id>")
	}
	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("usage: bonsai attach <id>")
	}
	return client.For(repoDir).Logs(id, true, 200, "", false, func(chunk string) error {
		_, werr := io.WriteString(out, chunk)
		return werr
	})
}

// cmdDaemon controls the daemon: status [--all], stop [--force].
func cmdDaemon(repoDir string, args []string, out io.Writer) error {
	action := "status"
	if len(args) > 0 {
		action = args[0]
		args = args[1:]
	}
	switch action {
	case "status":
		fs := flag.NewFlagSet("daemon status", flag.ContinueOnError)
		fs.SetOutput(out)
		all := fs.Bool("all", false, "list every live daemon")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *all {
			daemons, err := procstore.ListDaemons()
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "REPO\tPID\tPROCS")
			for _, d := range daemons {
				n := ""
				if resp, err := client.For(d.Root).Ping(); err == nil {
					n = strconv.Itoa(resp.ProcCount)
				}
				fmt.Fprintf(tw, "%s\t%d\t%s\n", d.Root, d.PID, n)
			}
			return tw.Flush()
		}
		resp, err := client.For(repoDir).Ping()
		if err != nil {
			fmt.Fprintln(out, "no daemon running for this repo")
			return nil
		}
		fmt.Fprintf(out, "daemon pid %d, protocol v%d, %d running process(es)\n", resp.PID, resp.Version, resp.ProcCount)
		return nil

	case "stop":
		fs := flag.NewFlagSet("daemon stop", flag.ContinueOnError)
		fs.SetOutput(out)
		force := fs.Bool("force", false, "stop even with running processes (kills them)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if err := client.For(repoDir).Shutdown(*force); err != nil {
			return err
		}
		fmt.Fprintln(out, "daemon stopped")
		return nil

	default:
		return fmt.Errorf("unknown daemon action %q (status|stop)", action)
	}
}

// readTail returns the last n bytes of the file at path (empty on error).
func readTail(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	start := int64(0)
	if fi.Size() > n {
		start = fi.Size() - n
	}
	if _, err := f.Seek(start, 0); err != nil {
		return ""
	}
	data, _ := io.ReadAll(f)
	return string(data)
}

// writePrefixed writes each line of chunk prefixed with "prefix: ".
func writePrefixed(out io.Writer, prefix, chunk string) error {
	for _, line := range splitLines(chunk) {
		if line == "" {
			continue
		}
		if _, err := fmt.Fprintf(out, "%s: %s\n", prefix, line); err != nil {
			return err
		}
	}
	return nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// humanDuration formats d compactly, e.g. "2h13m", "45s".
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}
