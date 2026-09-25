package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/gym"
)

func defaultGymService() (*gym.Service, error) {
	client := agym.NewClient("")
	cwd, _ := os.Getwd()
	repoDir, err := git.MainRoot(cwd)
	if err != nil {
		repoDir, _ = git.RepoRoot(cwd)
	}
	return gym.NewService(repoDir, client)
}

func cmdGym(args []string, out, errOut io.Writer) error {
	svc, err := defaultGymService()
	if err != nil {
		return err
	}
	return RunGym(args, svc, out, errOut)
}

// RunGym executes a `bonsai gym` subcommand with the provided service.
func RunGym(args []string, svc *gym.Service, out, errOut io.Writer) error {
	if len(args) == 0 {
		printGymUsage(out)
		return nil
	}

	switch args[0] {
	case "status":
		return cmdGymStatus(args[1:], svc, out, errOut)
	case "profiles":
		return cmdGymProfiles(args[1:], svc, out, errOut)
	case "usage":
		return cmdGymUsage(args[1:], svc, out, errOut)
	case "run":
		return cmdGymRun(args[1:], svc, out, errOut)
	case "stop":
		return cmdGymStop(args[1:], svc, out, errOut)
	case "attach":
		return cmdGymAttach(args[1:], svc, out, errOut)
	case "help", "-h", "--help":
		printGymUsage(out)
		return nil
	default:
		printGymUsage(errOut)
		return fmt.Errorf("unknown gym subcommand %q", args[0])
	}
}

func printGymUsage(w io.Writer) {
	fmt.Fprint(w, strings.TrimLeft(`
bonsai gym — manage AI agent integration

Usage:
  bonsai gym status [--worktree <path>] [--run <id>] [--json]
  bonsai gym profiles [--json]
  bonsai gym usage [--profile <name>] [--refresh] [--json]
  bonsai gym run [--worktree <path> | --here] [--branch <name>] [--profile <name>] (--task <text> | --task-file <path>) [--json]
  bonsai gym stop [--worktree <path>] [--run <id>] [--wait] [--timeout <dur>] [--json]
  bonsai gym attach [--worktree <path>] [--run <id>] [--after <seq>] [--json]
`, "\n"))
}

func cmdGymStatus(args []string, svc *gym.Service, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("gym status", flag.ContinueOnError)
	fs.SetOutput(errOut)
	wtFlag := fs.String("worktree", "", "worktree path")
	runID := fs.String("run", "", "lookup run by ID")
	jsonFlag := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	available := svc.Available()

	type statusOutput struct {
		Available bool            `json:"available"`
		Run       *agym.Run       `json:"run,omitempty"`
		Binding   *gym.RunBinding `json:"binding,omitempty"`
		Error     string          `json:"error,omitempty"`
	}

	resp := statusOutput{Available: available}

	if *runID != "" {
		if !available {
			if *jsonFlag {
				resp.Error = "agym unavailable"
				return json.NewEncoder(out).Encode(resp)
			}
			return errors.New("agym integration is not available or not installed")
		}
		run, err := svc.Client().GetRun(ctx, *runID)
		if err != nil {
			if *jsonFlag {
				resp.Error = err.Error()
				return json.NewEncoder(out).Encode(resp)
			}
			return err
		}
		resp.Run = run
		if *jsonFlag {
			return json.NewEncoder(out).Encode(resp)
		}
		fmt.Fprintf(out, "Run: %s\nStatus: %s\nProfile: %s\nTask: %s\n", run.RunID, run.Status, run.SelectedProfile, run.Task)
		return nil
	}

	targetPath := *wtFlag
	if targetPath == "" {
		cwd, err := os.Getwd()
		if err == nil {
			root, err := git.RepoRoot(cwd)
			if err == nil {
				targetPath = root
			}
		}
	}

	if targetPath != "" && svc.Store() != nil {
		view, err := svc.GetAgentView(ctx, targetPath)
		if err == nil && view != nil {
			resp.Binding = &view.Binding
			resp.Run = view.Run
			if view.Error != "" {
				resp.Error = view.Error
			}
		}
	}

	if *jsonFlag {
		return json.NewEncoder(out).Encode(resp)
	}

	if !available {
		fmt.Fprintln(out, "AGYM status: unavailable (install agym or verify PATH)")
		if resp.Binding != nil {
			fmt.Fprintf(out, "Existing run binding: %s (submission: %s)\n", resp.Binding.RequestID, resp.Binding.Submission)
		}
		return nil
	}

	fmt.Fprintln(out, "AGYM status: available")
	if resp.Run != nil {
		fmt.Fprintf(out, "Active run: %s\nStatus: %s\nProfile: %s\nTask: %s\n", resp.Run.RunID, resp.Run.Status, resp.Run.SelectedProfile, resp.Run.Task)
	} else if resp.Binding != nil {
		fmt.Fprintf(out, "Run binding: %s (%s)\n", resp.Binding.RequestID, resp.Binding.Submission)
	} else {
		fmt.Fprintln(out, "No active agent run in current worktree.")
	}
	return nil
}

func cmdGymProfiles(args []string, svc *gym.Service, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("gym profiles", flag.ContinueOnError)
	fs.SetOutput(errOut)
	jsonFlag := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !svc.Available() {
		return errors.New("agym integration is not available or not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	profiles, err := svc.Profiles(ctx)
	if err != nil {
		return err
	}

	if *jsonFlag {
		return json.NewEncoder(out).Encode(profiles)
	}

	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tREADINESS\tREASON")
	for _, p := range profiles {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", p.Name, p.Readiness, p.Reason)
	}
	return tw.Flush()
}

func cmdGymUsage(args []string, svc *gym.Service, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("gym usage", flag.ContinueOnError)
	fs.SetOutput(errOut)
	profile := fs.String("profile", "", "profile name")
	refresh := fs.Bool("refresh", false, "force quota refresh")
	jsonFlag := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !svc.Available() {
		return errors.New("agym integration is not available or not installed")
	}

	timeout := 10 * time.Second
	if *refresh {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	usages, err := svc.Usage(ctx, *profile, *refresh)
	if err != nil {
		return err
	}

	if *jsonFlag {
		return json.NewEncoder(out).Encode(usages)
	}

	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "PROFILE\tWINDOW\tREMAINING")
	for _, u := range usages {
		for _, w := range u.Windows {
			rem := "unknown"
			if w.Remaining != nil {
				rem = fmt.Sprintf("%.0f%%", *w.Remaining*100)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", u.ProfileID, w.Name, rem)
		}
	}
	return tw.Flush()
}

func cmdGymRun(args []string, svc *gym.Service, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("gym run", flag.ContinueOnError)
	fs.SetOutput(errOut)
	wtFlag := fs.String("worktree", "", "target worktree path")
	branchFlag := fs.String("branch", "", "branch name for new worktree (skips AI branch generation)")
	hereFlag := fs.Bool("here", false, "run in current worktree instead of auto-creating a new one")
	profile := fs.String("profile", "", "profile name (defaults to config or auto)")
	task := fs.String("task", "", "task description string")
	taskFile := fs.String("task-file", "", "path to task description file ('-' for stdin)")
	jsonFlag := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if (*task == "" && *taskFile == "") || (*task != "" && *taskFile != "") {
		return errors.New("must specify exactly one of --task or --task-file")
	}

	var taskText string
	if *task != "" {
		taskText = *task
	} else if *taskFile == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		taskText = string(b)
	} else {
		b, err := os.ReadFile(*taskFile)
		if err != nil {
			return fmt.Errorf("reading task file %s: %w", *taskFile, err)
		}
		taskText = string(b)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}

	// Case 1: Run in an explicit existing worktree (via --worktree or --here)
	if *wtFlag != "" || *hereFlag {
		targetPath := *wtFlag
		if targetPath == "" {
			root, err := git.RepoRoot(cwd)
			if err != nil {
				return fmt.Errorf("not in a git repository: %w", err)
			}
			targetPath = root
		}

		if *profile == "" {
			if cfg, err := config.LoadFor(targetPath); err == nil && cfg.Gym.DefaultProfile != "" {
				*profile = cfg.Gym.DefaultProfile
			} else {
				*profile = "auto"
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		run, err := svc.Start(ctx, targetPath, *profile, taskText)
		if err != nil {
			return err
		}

		if *jsonFlag {
			return json.NewEncoder(out).Encode(run)
		}

		fmt.Fprintf(out, "Started agent run %s (profile: %s, status: %s)\n", run.RunID, run.SelectedProfile, run.Status)
		fmt.Fprintf(out, "Attach with: bonsai gym attach --run %s\n", run.RunID)
		return nil
	}

	// Case 2: Auto-create branch and worktree from task (default behavior)
	repoDir, err := git.MainRoot(cwd)
	if err != nil {
		repoDir, err = git.RepoRoot(cwd)
		if err != nil {
			return fmt.Errorf("not in a git repository: %w", err)
		}
	}

	cfg, _ := config.LoadFor(repoDir)
	if *profile == "" {
		if cfg != nil && cfg.Gym.DefaultProfile != "" {
			*profile = cfg.Gym.DefaultProfile
		} else {
			*profile = "auto"
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := svc.StartAutoWorktree(ctx, *profile, taskText, *branchFlag, cfg)
	if err != nil && res == nil {
		return err
	}

	if *jsonFlag {
		type jsonRunResp struct {
			Branch   string    `json:"branch"`
			Worktree string    `json:"worktree"`
			Run      *agym.Run `json:"run,omitempty"`
			Error    string    `json:"error,omitempty"`
		}
		resp := jsonRunResp{
			Branch:   res.Branch,
			Worktree: res.WorktreePath,
			Run:      res.Run,
		}
		if err != nil {
			resp.Error = err.Error()
		}
		return json.NewEncoder(out).Encode(resp)
	}

	fmt.Fprintf(out, "Created branch '%s' and worktree '%s'\n", res.Branch, res.WorktreePath)
	if err != nil {
		return err
	}
	if res.Run != nil {
		fmt.Fprintf(out, "Started agent run %s (profile: %s, status: %s)\n", res.Run.RunID, res.Run.SelectedProfile, res.Run.Status)
		fmt.Fprintf(out, "Attach with: bonsai gym attach --run %s\n", res.Run.RunID)
	}
	return nil
}

func cmdGymStop(args []string, svc *gym.Service, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("gym stop", flag.ContinueOnError)
	fs.SetOutput(errOut)
	wtFlag := fs.String("worktree", "", "worktree path")
	runID := fs.String("run", "", "run ID to stop")
	wait := fs.Bool("wait", false, "wait for confirmed terminal state")
	timeout := fs.Duration("timeout", 30*time.Second, "wait timeout")
	jsonFlag := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	targetPath := *wtFlag
	if targetPath == "" && *runID == "" {
		cwd, err := os.Getwd()
		if err == nil {
			if root, err := git.RepoRoot(cwd); err == nil {
				targetPath = root
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	err := svc.Stop(ctx, targetPath, *runID)
	if err != nil {
		return err
	}

	if *wait && *runID != "" {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return fmt.Errorf("timeout waiting for run %s to stop", *runID)
			case <-ticker.C:
				run, err := svc.Client().GetRun(ctx, *runID)
				if err == nil && run != nil && agym.IsTerminal(run.Status) {
					if *jsonFlag {
						return json.NewEncoder(out).Encode(run)
					}
					fmt.Fprintf(out, "Run %s confirmed %s.\n", run.RunID, run.Status)
					return nil
				}
			}
		}
	}

	if *jsonFlag {
		resp := map[string]any{"ok": true, "stopped": true}
		return json.NewEncoder(out).Encode(resp)
	}

	fmt.Fprintln(out, "Stop requested successfully.")
	return nil
}

func cmdGymAttach(args []string, svc *gym.Service, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("gym attach", flag.ContinueOnError)
	fs.SetOutput(errOut)
	wtFlag := fs.String("worktree", "", "worktree path")
	runID := fs.String("run", "", "run ID to attach")
	after := fs.Uint64("after", 0, "event sequence number to resume after")
	jsonFlag := fs.Bool("json", false, "output NDJSON events")
	if err := fs.Parse(args); err != nil {
		return err
	}

	targetPath := *wtFlag
	if targetPath == "" && *runID == "" {
		cwd, err := os.Getwd()
		if err == nil {
			if root, err := git.RepoRoot(cwd); err == nil {
				targetPath = root
			}
		}
	}

	ctx := context.Background()

	if *jsonFlag {
		// Output raw NDJSON events
		targetRunID := *runID
		if targetRunID == "" && svc.Store() != nil && targetPath != "" {
			ws, err := svc.Store().ResolveWorkspaceIdentity(targetPath)
			if err == nil {
				binding, err := svc.Store().GetBinding(ws.WorktreeID)
				if err == nil && binding != nil {
					targetRunID = binding.RunID
				}
			}
		}
		if targetRunID == "" {
			return errors.New("no active run ID found to attach")
		}
		eventsCh, errCh := svc.Client().StreamEvents(ctx, targetRunID, *after)
		for {
			select {
			case err, ok := <-errCh:
				if ok && err != nil {
					return err
				}
			case ev, ok := <-eventsCh:
				if !ok {
					return nil
				}
				_ = json.NewEncoder(out).Encode(ev)
			}
		}
	}

	return svc.Attach(ctx, targetPath, *runID, *after, out)
}
