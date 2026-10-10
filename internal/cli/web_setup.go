package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/clipboard"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/server/localapi"
	websetupui "github.com/Tiago-0liveira/bonsai/internal/ui/websetup"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

// runSetup runs the setup TUI. From `bonsai web` (the first run) the wizard
// ends with bonsai web running; afterwards the usual summary is printed so
// the URL stays in the scrollback once the full-screen UI is gone.
func (w *webCLI) runSetup(mode websetupui.Mode, saved config.WebConfig, exists bool, opts webStartOptions) error {
	rootsPath, err := config.ProjectRootsPath()
	if err != nil {
		return err
	}
	roots, err := config.ReadProjectRoots(rootsPath)
	if err != nil {
		return w.fail(webFailure{
			headline: "bonsai web setup could not read your project folders",
			detail:   err.Error(),
			fix:      "fix or delete " + rootsPath + ", then run bonsai web setup again",
		})
	}
	home, _ := os.UserHomeDir()
	thisRepo := ""
	if cwd, err := os.Getwd(); err == nil {
		if main, err := coregit.MainRoot(cwd); err == nil {
			thisRepo, _ = config.CanonicalDirectory(main)
		}
	}
	info := websetupui.Info{
		Home:             home,
		ConfigPath:       w.configPath,
		StandardInterval: humanInterval(localapi.StandardUpdateInterval),
		HostedURL:        hostedWebURL(),
		DiscoveryDepth:   config.ProjectDiscoveryDepth,
		Running:          w.running(),
	}
	draft := setup.NewDraft(saved, exists, roots.Roots, config.SuggestedProjectRoots(home, ""), thisRepo)

	backend := w.newSetupBackend(rootsPath, saved, exists, opts)
	model := websetupui.New(backend, info, mode, draft, websetupui.NewRenderer(w.out))
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInput(w.in), tea.WithOutput(w.out))
	final, err := program.Run()
	if err != nil {
		return fmt.Errorf("setup screen failed: %w", err)
	}
	result, _ := final.(websetupui.Model)
	// The last Apply decides: an earlier success does not hide a later
	// failure (its stack may already be stopped).
	if failure := backend.lastFailure(); failure != nil {
		return w.fail(*failure)
	}
	if !result.Applied() {
		if backend.wasSaved() {
			fmt.Fprintln(w.out, "Settings saved. Start bonsai web with: bonsai web")
			return nil
		}
		fmt.Fprintln(w.errOut, "Setup cancelled; nothing changed.")
		if mode == websetupui.Wizard {
			fmt.Fprintln(w.errOut, "Start with the default settings instead: bonsai web --no-setup")
			return &ExitError{Code: 1}
		}
		return nil
	}
	cfg, _, err := config.ReadWebConfig(w.configPath)
	if err != nil {
		return err
	}
	if group := backend.lastGroup(); group != nil {
		fmt.Fprintln(w.out, "✓ bonsai web is running")
		w.printAccess(cfg, group)
		fmt.Fprintln(w.out, "  stop      bonsai web stop   ·   status: bonsai web status")
		if opts.attach {
			return w.follow(group)
		}
		return nil
	}
	fmt.Fprintf(w.out, "✓ Settings saved to %s\n", w.configPath)
	return nil
}

// running describes the running stack for the setup screens, or nil.
func (w *webCLI) running() *setup.Running {
	group, err := w.group(false)
	if err != nil || group == nil || group.State != "ready" {
		return nil
	}
	return &setup.Running{APIPort: group.APIPort, Hosted: group.BrowserOrigin != ""}
}

// setupBackend is what the setup TUI may do to this computer. Every write
// happens in Apply, which the TUI only calls from the Review screen.
type setupBackend struct {
	w         *webCLI
	opts      webStartOptions
	rootsPath string
	revision  uint64 // web.json revision the setup started from
	exists    bool
	env       checks.Env

	mu      sync.Mutex
	group   *procstore.ServeGroup
	failure *webFailure
	saved   bool
}

func (w *webCLI) newSetupBackend(rootsPath string, saved config.WebConfig, exists bool, opts webStartOptions) *setupBackend {
	return &setupBackend{
		w:         w,
		opts:      opts,
		rootsPath: rootsPath,
		revision:  saved.Revision,
		exists:    exists,
		env:       w.checksEnv(rootsPath),
	}
}

func (b *setupBackend) lastGroup() *procstore.ServeGroup {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.group
}

func (b *setupBackend) lastFailure() *webFailure {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failure
}

func (b *setupBackend) wasSaved() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saved
}

func (b *setupBackend) Checks(ctx context.Context, cfg config.WebConfig) []checks.Check {
	return checks.Run(ctx, b.env, checks.Options{APIPort: cfg.APIPort, UpdatesMode: cfg.Updates.Mode})
}

func (b *setupBackend) ScanFolder(ctx context.Context, path string) (string, []setup.Repo, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	scan, err := localapi.ScanProjectFolder(ctx, path)
	if err != nil {
		return "", nil, err
	}
	repos := make([]setup.Repo, 0, len(scan.Repos))
	for _, r := range scan.Repos {
		repos = append(repos, setup.Repo{ID: r.ID, Name: r.Name, Path: r.Path})
	}
	if !scan.Available && len(scan.Messages) > 0 {
		return scan.Path, repos, errors.New(scan.Messages[0])
	}
	return scan.Path, repos, nil
}

// FixCommand runs only the inline fixes the checks engine marks as such
// (interactive, user-level logins). Their command lines are fixed strings,
// never user input.
func (b *setupBackend) FixCommand(fix checks.Fix) *exec.Cmd {
	if !fix.Inline {
		return nil
	}
	fields := strings.Fields(fix.Command)
	if len(fields) == 0 {
		return nil
	}
	return exec.Command(fields[0], fields[1:]...)
}

func (b *setupBackend) Open(url string) error  { return b.w.openURL(url) }
func (b *setupBackend) Copy(text string) error { return clipboard.Copy(text) }

// lineWriter turns lines written by the start path ("Restarting …",
// "Stopped …") into progress steps instead of letting them reach the
// terminal under the full-screen UI.
type lineWriter struct {
	mu   sync.Mutex
	n    int
	emit func(websetupui.Step)
	buf  string
}

func (l *lineWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf += string(p)
	for {
		line, rest, ok := strings.Cut(l.buf, "\n")
		if !ok {
			break
		}
		l.buf = rest
		if line = strings.TrimSpace(line); line != "" {
			l.n++
			l.emit(websetupui.Step{ID: fmt.Sprintf("note-%d", l.n), Label: line, State: websetupui.StepDone})
		}
	}
	return len(p), nil
}

func (b *setupBackend) Apply(ctx context.Context, plan setup.Plan, progress func(websetupui.Step)) websetupui.Result {
	quiet := *b.w
	notes := &lineWriter{emit: progress}
	quiet.out, quiet.errOut = notes, notes
	home, _ := os.UserHomeDir()

	b.mu.Lock()
	b.failure = nil
	b.mu.Unlock()
	// fail reports a failed step and keeps it for the summary printed once
	// the setup screen closes. failure, when set, is the start path's own
	// "could not start" block.
	fail := func(id, label, detail, fix string, failure *webFailure) websetupui.Result {
		if failure == nil {
			failure = &webFailure{headline: "bonsai web setup could not apply your settings", process: "step", detail: label + ": " + detail, fix: fix}
		}
		b.mu.Lock()
		b.failure = failure
		b.mu.Unlock()
		progress(websetupui.Step{ID: id, Label: label, State: websetupui.StepFailed, Detail: detail, Fix: fix})
		return websetupui.Result{}
	}

	// 1. Settings.
	progress(websetupui.Step{ID: "save", Label: "Saving settings", State: websetupui.StepRunning})
	expected := b.revision
	if !b.exists {
		expected = ^uint64(0)
	}
	cfg, err := config.UpdateWebConfig(b.w.configPath, expected, func(c *config.WebConfig) error {
		revision := c.Revision
		*c = plan.Config
		c.Revision = revision
		return nil
	})
	if errors.Is(err, config.ErrWebConfigRevision) {
		return fail("save", "Save settings", "your settings changed in another window since this setup opened", "close this setup and run bonsai web setup again", nil)
	}
	if err != nil {
		return fail("save", "Save settings", err.Error(), "fix or delete "+b.w.configPath+", then run bonsai web setup again", nil)
	}
	b.mu.Lock()
	b.saved, b.revision, b.exists = true, cfg.Revision, true
	b.mu.Unlock()
	progress(websetupui.Step{ID: "save", Label: "Saved settings", State: websetupui.StepDone, Detail: setup.TildePath(home, b.w.configPath)})

	// 2. Project folders and the repositories to show.
	if plan.ProjectsChanged() {
		progress(websetupui.Step{ID: "projects", Label: "Updating project folders", State: websetupui.StepRunning})
		shown, err := b.applyProjects(ctx, plan)
		if err != nil {
			return fail("projects", "Update project folders", err.Error(), "run bonsai web setup again, or change folders in the browser: Settings → Projects", nil)
		}
		detail := fmt.Sprintf("%d added, %d removed", len(plan.AddRoots), len(plan.RemoveRoots))
		if shown > 0 {
			detail += " · " + countWord(shown, "repo") + " shown"
		}
		progress(websetupui.Step{ID: "projects", Label: "Updated project folders", State: websetupui.StepDone, Detail: detail})
	}

	// 3. Start, restart or leave alone.
	opts := b.opts
	opts.applySettings = true
	if !plan.Start && plan.RestartPort != 0 {
		// Restart where the user wants it: their new port, or the port it
		// runs on now (a `bonsai web --port N` stack stays on N).
		opts.port, opts.portFlag = plan.RestartPort, true
	}
	result := websetupui.Result{OK: true}
	switch {
	case plan.Start || plan.RestartAPI:
		id, label, failed, done := "start", "Starting Bonsai", "Start Bonsai", "Started Bonsai"
		if !plan.Start {
			id, label, failed, done = "restart", "Restarting the Bonsai API", "Restart the Bonsai API", "Restarted the Bonsai API"
		}
		progress(websetupui.Step{ID: id, Label: label, State: websetupui.StepRunning})
		group, _, failure, err := quiet.ensure(cfg, opts)
		if err != nil {
			failure = &webFailure{detail: err.Error(), fix: "bonsai web --attach   to watch it start"}
		}
		if failure != nil {
			b.mu.Lock()
			b.group = nil // the previous stack may already be stopped
			b.mu.Unlock()
			detail := failure.detail
			if detail == "" {
				detail = failure.headline
			}
			return fail(id, failed, detail, failure.fix, failure)
		}
		b.mu.Lock()
		b.group = group
		b.mu.Unlock()
		result.URL, result.Port = webUIURL(cfg, group.APIPort), group.APIPort
		progress(websetupui.Step{ID: id, Label: done, State: websetupui.StepDone, Detail: localWebOrigin(group.APIPort)})
	default:
		if group, err := b.w.group(false); err == nil && group != nil && group.State == "ready" {
			b.mu.Lock()
			b.group = group
			b.mu.Unlock()
			result.URL, result.Port = webUIURL(cfg, group.APIPort), group.APIPort
			if plan.ProjectsChanged() {
				result.Notes = append(result.Notes, "Bonsai picks up project changes within 30 s.")
			}
		} else {
			b.mu.Lock()
			b.group = nil
			b.mu.Unlock()
			result.Notes = append(result.Notes, "bonsai web is not running. Start it with: bonsai web")
		}
	}

	// 4. The browser, on the first run only.
	if plan.Start && cfg.OpenBrowser && !b.opts.noOpen && result.URL != "" {
		if err := b.w.openURL(result.URL); err != nil {
			progress(websetupui.Step{ID: "open", Label: "Open your browser", State: websetupui.StepSkipped, Detail: "could not open one; open " + result.URL + " yourself"})
		} else {
			result.Opened = true
			progress(websetupui.Step{ID: "open", Label: "Opened your browser", State: websetupui.StepDone})
		}
	}
	return result
}

// applyProjects adds and removes project roots and selects every repository
// found in the added folders. It returns how many repositories it selected.
func (b *setupBackend) applyProjects(ctx context.Context, plan setup.Plan) (int, error) {
	roots, err := config.ReadProjectRoots(b.rootsPath)
	if err != nil {
		return 0, err
	}
	revision := roots.Revision
	change := func(add, remove string) error {
		next, err := config.UpdateProjectRoots(b.rootsPath, idempotencyKey(), revision, add, remove)
		if err != nil {
			return err
		}
		revision = next.Revision
		return nil
	}
	present := map[string]bool{}
	for _, root := range roots.Roots {
		present[root.ID] = true
	}
	for _, root := range plan.RemoveRoots {
		if !present[root.ID] {
			continue // already gone (a retry, or removed elsewhere)
		}
		if err := change("", root.ID); err != nil {
			return 0, fmt.Errorf("remove %s: %w", root.Path, err)
		}
	}
	ids := append([]string{}, plan.SelectRepos...)
	for _, path := range plan.AddRoots {
		if err := change(path, ""); err != nil {
			return 0, fmt.Errorf("add %s: %w", path, err)
		}
		// Scan again: the screen may have moved on before its scan ended.
		_, repos, err := b.ScanFolder(ctx, path)
		if err == nil {
			for _, r := range repos {
				ids = append(ids, r.ID)
			}
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	added := map[string]bool{}
	_, err = config.UpdateProjectSelection(config.ProjectSelectionPath(b.rootsPath), func(current config.ProjectSelection) ([]string, error) {
		for _, id := range current.Selected {
			added[id] = false
		}
		for _, id := range ids {
			if _, ok := added[id]; !ok {
				added[id] = true
			}
		}
		return append(current.Selected, ids...), nil
	})
	count := 0
	for _, isNew := range added {
		if isNew {
			count++
		}
	}
	return count, err
}

func idempotencyKey() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return "web-setup-" + hex.EncodeToString(raw[:])
}

// checksEnv wires the checks engine to this computer: real tools, the API
// port as bonsai web sees it, and discovery under the project roots.
func (w *webCLI) checksEnv(rootsPath string) checks.Env {
	env := checks.SystemEnv()
	env.Port = w.portStatus
	env.Projects = func(ctx context.Context) (checks.ProjectsSummary, error) {
		return projectsSummary(ctx, rootsPath)
	}
	return env
}

func (w *webCLI) portStatus(port int) checks.PortStatus {
	use := inspectPort(port, w.home)
	if use.free {
		return checks.PortStatus{Free: true}
	}
	if group, err := w.group(false); err == nil && group != nil && group.APIPort == port {
		return checks.PortStatus{Web: true}
	}
	if g := use.legacy; g != nil && g.mode != procstore.ServeModeDevelopment {
		// bonsai web stops the previous per-repo API and takes over.
		return checks.PortStatus{Replaceable: "the per-repo local API of " + g.workspace}
	}
	use = describePortOwner(port, use)
	status := checks.PortStatus{Bonsai: use.bonsai || use.legacy != nil}
	switch {
	case use.legacy != nil:
		status.Owner = "the development stack of " + use.legacy.workspace
	case use.known && use.owner.Name != "":
		status.Owner = fmt.Sprintf("pid %d, %s", use.owner.PID, use.owner.Name)
	case use.known:
		status.Owner = fmt.Sprintf("pid %d", use.owner.PID)
	}
	return status
}

// projectsSummary counts what discovery finds under the project roots and
// how much of it is selected to show.
func projectsSummary(ctx context.Context, rootsPath string) (checks.ProjectsSummary, error) {
	roots, err := config.ReadProjectRoots(rootsPath)
	if err != nil {
		return checks.ProjectsSummary{}, err
	}
	selection, err := config.ReadProjectSelection(config.ProjectSelectionPath(rootsPath))
	if err != nil {
		return checks.ProjectsSummary{}, err
	}
	selected := map[string]bool{}
	for _, id := range selection.Selected {
		selected[id] = true
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	home, _ := os.UserHomeDir()
	out := checks.ProjectsSummary{Folders: len(roots.Roots)}
	// Same rules and parallelism as the API's discovery: a root that now
	// resolves somewhere else is unavailable, not scanned at its new target.
	scans := make([]localapi.FolderScan, len(roots.Roots))
	usable := make([]bool, len(roots.Roots))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				scan, err := localapi.ScanProjectFolder(ctx, roots.Roots[i].Path)
				scans[i], usable[i] = scan, err == nil && scan.Available && scan.Path == roots.Roots[i].Path
			}
		}()
	}
	for i := range roots.Roots {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	found := map[string]bool{}
	for i, root := range roots.Roots {
		if !usable[i] {
			out.Unavailable = append(out.Unavailable, setup.TildePath(home, filepath.Clean(root.Path)))
			continue
		}
		for _, r := range scans[i].Repos {
			found[r.ID] = true
		}
	}
	out.Found = len(found)
	for id := range found {
		if selected[id] {
			out.Shown++
		}
	}
	return out, nil
}
