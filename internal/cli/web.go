package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/Tiago-0liveira/bonsai/internal/core/browser"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	"github.com/Tiago-0liveira/bonsai/internal/server/localapi"
	websetupui "github.com/Tiago-0liveira/bonsai/internal/ui/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/version"
)

// ExitError ends the process with Code after the command has already
// explained the failure, so the caller prints nothing more.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

const webUsage = `usage: bonsai web [--attach] [--no-open] [--no-setup] [--port N]
       bonsai web setup|doctor|status|open|attach|stop
       bonsai web logs [api|tunnel] [-f|--follow] [-n N] [--grep TEXT] [-i]
       bonsai web restart [api|tunnel]`

// webCLI is one `bonsai web` invocation. The stack it controls is the single
// user-level serve group "web", supervised by a daemon whose home is
// config.WebHome() rather than a repository.
type webCLI struct {
	in          io.Reader
	out, errOut io.Writer
	home        string
	configPath  string
	client      *client.Client
	openURL     func(string) error
	tty         bool
	// executable is the bonsai binary the daemon runs for the API; empty
	// means this process's own executable.
	executable string
}

func newWebCLI(in io.Reader, out, errOut io.Writer) (*webCLI, error) {
	home, err := config.WebHome()
	if err != nil {
		return nil, fmt.Errorf("cannot locate the bonsai web state directory: %w", err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create %s: %w", home, err)
	}
	configPath, err := config.WebConfigPath()
	if err != nil {
		return nil, fmt.Errorf("cannot locate the bonsai web settings: %w", err)
	}
	return &webCLI{
		in: in, out: out, errOut: errOut,
		home:       home,
		configPath: configPath,
		client:     client.ForUserHome(home),
		openURL:    browser.Open,
		tty:        isTTY(in) && isTTY(out),
	}, nil
}

func isTTY(stream any) bool {
	f, ok := stream.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

type webStartOptions struct {
	attach   bool
	noOpen   bool
	noSetup  bool
	port     int
	portFlag bool // --port was given explicitly
	// Set by the deprecated `bonsai serve` alias from a repository's
	// .bonsai.yaml. A preferred port only applies when bonsai web is not
	// already running; it never makes the shared stack fail.
	preferredPort   int
	startupTimeout  int
	shutdownTimeout int
	// Set by `bonsai web setup` when applying new settings: a running stack
	// on another port is restarted on the configured one.
	applySettings bool
}

func cmdWeb(args []string, in io.Reader, out, errOut io.Writer) error {
	w, err := newWebCLI(in, out, errOut)
	if err != nil {
		return err
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, rest := args[0], args[1:]
		switch sub {
		case "setup":
			return w.setup(rest)
		case "status":
			return w.noArgs("status", rest, w.status)
		case "open":
			return w.noArgs("open", rest, w.open)
		case "logs":
			return w.logs(rest)
		case "attach":
			return w.noArgs("attach", rest, w.attach)
		case "restart":
			return w.restart(rest)
		case "stop":
			return w.noArgs("stop", rest, w.stop)
		case "doctor":
			return w.noArgs("doctor", rest, w.doctor)
		default:
			return fmt.Errorf("unknown bonsai web command %q\n%s", sub, webUsage)
		}
	}
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var opts webStartOptions
	fs.BoolVar(&opts.attach, "attach", false, "stay in the foreground with the live log viewer")
	fs.BoolVar(&opts.noOpen, "no-open", false, "do not open the browser")
	fs.BoolVar(&opts.noSetup, "no-setup", false, "never run the setup flow; use saved or default settings")
	fs.IntVar(&opts.port, "port", 0, "local API port (default from web settings, 7001)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%s", webUsage)
	}
	fs.Visit(func(f *flag.Flag) { opts.portFlag = opts.portFlag || f.Name == "port" })
	if opts.portFlag && (opts.port < 1 || opts.port > 65535) {
		return fmt.Errorf("--port must be between 1 and 65535")
	}
	return w.start(opts)
}

func (w *webCLI) noArgs(name string, args []string, run func() error) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: bonsai web %s", name)
	}
	return run()
}

// group returns the running web serve group, or nil when none exists. It
// never starts the web daemon just to answer "nothing is running".
//
// A web daemon left by another bonsai version (different daemon protocol)
// only ever runs this stack. With replace set (start, stop) it is shut down,
// processes included, and reported as absent; otherwise the caller is told
// how to clear it.
func (w *webCLI) group(replace bool) (*procstore.ServeGroup, error) {
	if _, err := w.client.Ping(); err != nil {
		return nil, nil
	}
	group, err := w.client.ServeStatus(procstore.WebServeGroupID)
	if !errors.Is(err, client.ErrIncompatibleDaemon) {
		return group, err
	}
	if !replace {
		return nil, errors.New("bonsai web was started by a different bonsai version. Stop it with: bonsai web stop")
	}
	if err := w.client.Shutdown(true); err != nil {
		return nil, fmt.Errorf("could not stop bonsai web from a different bonsai version: %w", err)
	}
	fmt.Fprintln(w.out, "Stopped bonsai web from a different bonsai version.")
	return nil, nil
}

// settle waits while the group is still starting (another `bonsai web`, or
// the daemon restarting a crashed API) so it is neither torn down nor
// mistaken for a foreign program on the port.
func (w *webCLI) settle(group *procstore.ServeGroup, timeout time.Duration) *procstore.ServeGroup {
	deadline := time.Now().Add(timeout)
	for group != nil && group.State == "starting" && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		next, err := w.client.ServeStatus(procstore.WebServeGroupID)
		if err != nil {
			return group
		}
		group = next
	}
	return group
}

func (w *webCLI) settings() (config.WebConfig, error) {
	cfg, created, err := config.EnsureWebConfig(w.configPath)
	if err != nil {
		return cfg, err
	}
	if created {
		fmt.Fprintf(w.out, "Saved default settings to %s\nCustomize with: bonsai web setup\n\n", w.configPath)
	}
	return cfg, nil
}

func (w *webCLI) start(opts webStartOptions) error {
	saved, exists, err := config.ReadWebConfig(w.configPath)
	if err != nil {
		return w.fail(webFailure{
			detail: err.Error(),
			fix:    "fix or delete " + w.configPath + ", then run bonsai web again",
		})
	}
	if wantsSetup(w.tty, opts.noSetup, exists, saved.SetupVersion) {
		if err := projectRootsReadable(); err != nil {
			// The setup cannot show folders it cannot read; start as before
			// and let the API report the broken file.
			fmt.Fprintf(w.errOut, "Skipping the guided setup: %v\nFix that file, then run: bonsai web setup\n\n", err)
		} else {
			return w.runSetup(websetupui.Wizard, saved, exists, opts)
		}
	}
	cfg, err := w.settings()
	if err != nil {
		return w.fail(webFailure{
			detail: err.Error(),
			fix:    "fix or delete " + w.configPath + ", then run bonsai web again",
		})
	}
	group, reused, failure, err := w.ensure(cfg, opts)
	if err != nil {
		return err
	}
	if failure != nil {
		return w.fail(*failure)
	}
	return w.started(cfg, opts, group, reused)
}

func projectRootsReadable() error {
	path, err := config.ProjectRootsPath()
	if err != nil {
		return err
	}
	_, err = config.ReadProjectRoots(path)
	return err
}

// wantsSetup decides whether `bonsai web` opens the guided setup first: on
// the first run, or when this build's setup asks something the saved settings
// predate, and only when someone is at the terminal to answer. --no-setup and
// scripts get the saved (or default) settings.
func wantsSetup(tty, noSetup, exists bool, setupVersion int) bool {
	return tty && !noSetup && (!exists || setupVersion < config.WebSetupVersion)
}

// ensure starts bonsai web with cfg, or reuses the running stack when it
// already matches. It reports a failure (the "could not start" block) rather
// than printing it, so the setup TUI can show it too. With
// opts.applySettings a running stack on another port is restarted on the
// configured one instead of being reported.
func (w *webCLI) ensure(cfg config.WebConfig, opts webStartOptions) (*procstore.ServeGroup, bool, *webFailure, error) {
	port := cfg.APIPort
	if opts.portFlag {
		port = opts.port
	}

	group, err := w.group(true)
	if err != nil {
		return nil, false, nil, err
	}
	group = w.settle(group, 30*time.Second)
	if group != nil && group.State == "ready" && !opts.portFlag && opts.preferredPort != 0 && group.APIPort != opts.preferredPort {
		fmt.Fprintf(w.errOut, "bonsai web is already running on port %d; serve.api_port %d from .bonsai.yaml is ignored.\n", group.APIPort, opts.preferredPort)
		port = group.APIPort
	} else if opts.preferredPort != 0 && !opts.portFlag {
		port = opts.preferredPort
	}
	browserOrigin := webBrowserOrigin(cfg)
	if group != nil && group.State == "ready" {
		// The API serves the UI, so reusing an API from another bonsai
		// version would bring back the version skew the local UI removes.
		switch running := runningAPIVersion(group.APIPort); {
		case group.APIPort != port && opts.applySettings:
			fmt.Fprintf(w.out, "Restarting bonsai web on port %d.\n", port)
		case group.APIPort != port:
			return nil, false, &webFailure{
				headline: fmt.Sprintf("bonsai web is already running on port %d", group.APIPort),
				fix:      fmt.Sprintf("bonsai web stop      then      bonsai web --port %d", port),
			}, nil
		case running != "" && running != version.String():
			fmt.Fprintf(w.out, "Restarting bonsai web %s to run %s.\n", running, version.String())
		case group.BrowserOrigin != browserOrigin:
			fmt.Fprintln(w.out, "Restarting bonsai web to apply changed settings (hosted app access).")
		default:
			return group, true, nil, nil
		}
	}
	if group != nil {
		// Our own group, but not healthy or not current: clear it so its API
		// does not read as "another program" on the port.
		if err := w.client.ServeStop(procstore.WebServeGroupID); err != nil {
			return nil, false, &webFailure{
				detail: "the previous bonsai web did not stop: " + err.Error(),
				fix:    "bonsai web stop      then      bonsai web",
			}, nil
		}
	}

	if failure, ok := w.preflightPort(port); !ok {
		return nil, false, &failure, nil
	}

	executable := w.executable
	if executable == "" {
		if executable, err = os.Executable(); err != nil {
			return nil, false, nil, err
		}
	}
	spec := procstore.ServeSpec{
		Mode:                   procstore.ServeModeProduction,
		Scope:                  procstore.ServeScopeUser,
		WorkspaceID:            procstore.WebServeGroupID,
		WorkspacePath:          w.home,
		Executable:             executable,
		APIPort:                port,
		BrowserOrigin:          browserOrigin,
		StartupTimeoutSeconds:  firstPositive(opts.startupTimeout, cfg.StartupTimeoutSeconds),
		ShutdownTimeoutSeconds: firstPositive(opts.shutdownTimeout, cfg.ShutdownTimeoutSeconds),
	}
	group, err = w.client.ServeStart(spec)
	if err != nil && strings.Contains(err.Error(), "is already in use") {
		// Something took the port between the preflight and the daemon's own
		// check. Diagnose again; if that cleared it, try once more.
		if failure, ok := w.preflightPort(port); !ok {
			return nil, false, &failure, nil
		}
		group, err = w.client.ServeStart(spec)
	}
	if err != nil {
		// The daemon already stopped whatever it had started; make sure no
		// half-started group survives a client-side surprise either.
		_ = w.client.ServeStop(procstore.WebServeGroupID)
		failure := w.startFailure(port, err)
		return nil, false, &failure, nil
	}
	return group, group.Reused, nil, nil
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func (w *webCLI) started(cfg config.WebConfig, opts webStartOptions, group *procstore.ServeGroup, reused bool) error {
	headline := "bonsai web is running"
	if reused {
		headline = "bonsai web is already running"
	}
	fmt.Fprintf(w.out, "✓ %s\n", headline)
	w.printAccess(cfg, group)
	fmt.Fprintln(w.out, "  stop      bonsai web stop   ·   status: bonsai web status")

	if cfg.OpenBrowser && !opts.noOpen {
		target := webUIURL(cfg, group.APIPort)
		if err := w.openURL(target); err != nil {
			fmt.Fprintf(w.errOut, "Could not open a browser (%v). Open %s yourself.\n", err, target)
		}
	}
	if opts.attach {
		return w.follow(group)
	}
	return nil
}

// printAccess describes the running group: its own UI (same origin as the
// API), and the hosted app only while the API actually allows it.
func (w *webCLI) printAccess(cfg config.WebConfig, group *procstore.ServeGroup) {
	fmt.Fprintf(w.out, "  UI        %s\n", webUIURL(cfg, group.APIPort))
	if !cfg.Interfaces.Local {
		fmt.Fprintf(w.out, "  API       %s\n", localWebOrigin(group.APIPort))
	} else if group.BrowserOrigin != "" {
		fmt.Fprintf(w.out, "  hosted    %s/app\n", group.BrowserOrigin)
	}
	fmt.Fprintf(w.out, "  updates   %s\n", webUpdatesText(cfg))
}

func localWebOrigin(port int) string { return "http://127.0.0.1:" + strconv.Itoa(port) }

// webUIURL is the page bonsai web prints and opens: the UI the local API
// serves itself, or the hosted app when the local interface is turned off.
func webUIURL(cfg config.WebConfig, port int) string {
	if !cfg.Interfaces.Local {
		return hostedWebURL()
	}
	return localWebOrigin(port) + "/app"
}

// webBrowserOrigin is the extra browser origin the API allows next to its
// own: the hosted app, or none when that interface is disabled.
func webBrowserOrigin(cfg config.WebConfig) string {
	if !cfg.Interfaces.Hosted {
		return ""
	}
	return hostedWebOrigin()
}

// runningAPIVersion asks a running API for its bonsai version ("" when it
// cannot tell). /version is a public probe: no Origin, no session.
func runningAPIVersion(port int) string {
	httpClient := &http.Client{Timeout: 2 * time.Second}
	resp, err := httpClient.Get(localWebOrigin(port) + "/version")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var body struct {
		Version string `json:"version"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body) != nil {
		return ""
	}
	return body.Version
}

func webUpdatesText(cfg config.WebConfig) string {
	standard := "standard (every ~" + humanInterval(localapi.StandardUpdateInterval) + ")"
	if cfg.Updates.Mode == config.WebUpdatesLive {
		return standard + "; live updates are not available in this version yet"
	}
	return standard
}

func humanInterval(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return strconv.Itoa(int(d/time.Minute)) + " min"
	}
	return strconv.Itoa(int(d.Round(time.Second)/time.Second)) + " s"
}

// follow keeps `--attach` in the foreground: the live log viewer on a
// terminal, plain followed logs otherwise (never an interactive prompt).
func (w *webCLI) follow(group *procstore.ServeGroup) error {
	if w.tty {
		return runServeTUI(w.in, w.out, w.client, group)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return w.client.ServeLogs(ctx, group.ID, "", true, 200, "", false, func(chunk string) error {
		_, err := io.WriteString(w.out, chunk)
		return err
	})
}

func (w *webCLI) status() error {
	group, err := w.group(false)
	if err != nil {
		return err
	}
	if group == nil {
		fmt.Fprintln(w.out, "bonsai web is not running. Start it with: bonsai web")
		return nil
	}
	cfg, _, err := config.ReadWebConfig(w.configPath)
	if err != nil {
		cfg = config.DefaultWebConfig()
	}
	fmt.Fprintf(w.out, "bonsai web · %s\n", group.State)
	w.printAccess(cfg, group)
	fmt.Fprintf(w.out, "  settings  %s\n\n", w.configPath)
	tw := tabwriter.NewWriter(w.out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "PROCESS\tSTATUS\tPID\tADDRESS")
	for _, process := range group.Processes {
		address := ""
		if process.ExpectedPort > 0 {
			address = "127.0.0.1:" + strconv.Itoa(process.ExpectedPort)
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", process.Name, process.State, process.PID, address)
	}
	return tw.Flush()
}

func (w *webCLI) open() error {
	group, err := w.group(false)
	if err != nil {
		return err
	}
	cfg, _, err := config.ReadWebConfig(w.configPath)
	if err != nil {
		cfg = config.DefaultWebConfig()
	}
	port := cfg.APIPort
	if group == nil {
		fmt.Fprintln(w.errOut, "bonsai web is not running; the page will not load until you run: bonsai web")
	} else {
		port = group.APIPort
	}
	target := webUIURL(cfg, port)
	if err := w.openURL(target); err != nil {
		fmt.Fprintf(w.errOut, "Could not open a browser (%v).\n", err)
		fmt.Fprintln(w.out, target)
	}
	return nil
}

func (w *webCLI) attach() error {
	group, err := w.group(false)
	if err != nil {
		return err
	}
	if group == nil {
		return errors.New("bonsai web is not running. Start it with: bonsai web --attach")
	}
	return w.follow(group)
}

func (w *webCLI) stop() error {
	group, err := w.group(true)
	if err != nil {
		return err
	}
	if group == nil {
		fmt.Fprintln(w.out, "bonsai web is not running")
		return nil
	}
	if err := w.client.ServeStop(procstore.WebServeGroupID); err != nil {
		return err
	}
	fmt.Fprintln(w.out, "✓ bonsai web stopped")
	return nil
}

func webProcessName(name string) (string, error) {
	switch name {
	case "", "api":
		return name, nil
	case "tunnel":
		return "", errors.New("there is no tunnel: live updates are not available in this version yet")
	}
	return "", fmt.Errorf("unknown process %q (expected api or tunnel)", name)
}

func (w *webCLI) restart(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: bonsai web restart [api|tunnel]")
	}
	name := ""
	if len(args) == 1 {
		name = args[0]
	}
	name, err := webProcessName(name)
	if err != nil {
		return err
	}
	group, err := w.group(false)
	if err != nil {
		return err
	}
	if group == nil {
		return errors.New("bonsai web is not running. Start it with: bonsai web")
	}
	if _, err := w.client.ServeRestart(procstore.WebServeGroupID, name); err != nil {
		return err
	}
	fmt.Fprintln(w.out, "✓ restarted")
	return w.status()
}

func (w *webCLI) logs(args []string) error {
	process := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		process, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("web logs", flag.ContinueOnError)
	fs.SetOutput(w.errOut)
	var follow bool
	fs.BoolVar(&follow, "f", false, "follow the logs")
	fs.BoolVar(&follow, "follow", false, "follow the logs")
	n := fs.Int("n", 200, "number of recent lines")
	processFlag := fs.String("process", "", "restrict to a process name (same as the positional name)")
	grep := fs.String("grep", "", "show lines containing text")
	insensitive := fs.Bool("i", false, "case-insensitive search")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 || (fs.NArg() == 1 && process != "") {
		return fmt.Errorf("usage: bonsai web logs [api|tunnel] [-f|--follow] [-n N] [--grep TEXT] [-i]")
	}
	if fs.NArg() == 1 {
		process = fs.Arg(0)
	}
	if process == "" {
		process = *processFlag
	}
	process, err := webProcessName(process)
	if err != nil {
		return err
	}
	write := func(chunk string) error {
		_, err := io.WriteString(w.out, chunk)
		return err
	}
	group, err := w.group(false)
	if err != nil {
		return err
	}
	if group == nil {
		// After a failed start (or a stop) the group is gone but each
		// process keeps its log; show the latest one so the hint printed on
		// failure always works.
		return w.lastProcessLog(process, *n, *grep, *insensitive, write)
	}
	ctx := context.Background()
	stop := func() {}
	if follow {
		ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt)
	}
	defer stop()
	return w.client.ServeLogs(ctx, procstore.WebServeGroupID, process, follow, *n, *grep, *insensitive, write)
}

func (w *webCLI) lastProcessLog(process string, n int, grep string, insensitive bool, write func(string) error) error {
	if process == "" {
		process = "api"
	}
	records, err := w.client.Store().ListRecords()
	if err != nil {
		return err
	}
	var latest *procstore.Record
	for _, record := range records {
		if record.ServeGroup == procstore.WebServeGroupID && record.ServeName == process && (latest == nil || record.ID > latest.ID) {
			latest = record
		}
	}
	if latest == nil {
		fmt.Fprintln(w.out, "bonsai web has no logs yet. Start it with: bonsai web")
		return nil
	}
	fmt.Fprintf(w.errOut, "bonsai web is not running; showing the last %s run.\n", process)
	data, err := w.client.Store().ReadCombinedLog(latest.ID)
	if err != nil {
		return err
	}
	mr := &procstore.MarkerRenderer{}
	if filtered := procstore.FilterLog(string(data), n, grep, insensitive); filtered != "" {
		return write(mr.Render(filtered))
	}
	return nil
}

func (w *webCLI) setup(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: bonsai web setup")
	}
	if !w.tty {
		cfg, err := w.settings()
		if err != nil {
			return err
		}
		raw, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(w.out, "bonsai web settings · %s\n\n%s\n\n", w.configPath, raw)
		fmt.Fprintln(w.out, "Run bonsai web setup in a terminal for the guided setup, or edit the file above")
		fmt.Fprintln(w.out, "and run bonsai web stop and bonsai web.")
		return nil
	}
	saved, exists, err := config.ReadWebConfig(w.configPath)
	if err != nil {
		return w.fail(webFailure{
			headline: "bonsai web setup could not read your settings",
			detail:   err.Error(),
			fix:      "fix or delete " + w.configPath + ", then run bonsai web setup again",
		})
	}
	mode := websetupui.Edit
	if !exists || saved.SetupVersion < config.WebSetupVersion {
		mode = websetupui.Wizard
	}
	return w.runSetup(mode, saved, exists, webStartOptions{})
}

// webFailure is the "✗ could not start" block: what failed, why, and one fix.
type webFailure struct {
	headline string
	process  string
	detail   string
	fix      string
	logs     bool
}

func (w *webCLI) fail(f webFailure) error {
	if f.headline == "" {
		f.headline = "bonsai web could not start"
	}
	fmt.Fprintf(w.errOut, "✗ %s\n", f.headline)
	if f.detail != "" {
		label := f.process
		if label == "" {
			label = "cause"
		}
		lines := strings.Split(f.detail, "\n")
		fmt.Fprintf(w.errOut, "  %-5s %s\n", label, lines[0])
		for _, line := range lines[1:] {
			fmt.Fprintf(w.errOut, "        %s\n", line)
		}
	}
	if f.fix != "" {
		fmt.Fprintf(w.errOut, "  fix   %s\n", f.fix)
	}
	if f.logs {
		fmt.Fprintln(w.errOut, "  logs  bonsai web logs api")
	}
	return &ExitError{Code: 1}
}

var exitCodePattern = regexp.MustCompile(`\(exit (-?\d+)\)`)

// startFailure turns a daemon ServeStart error into the failure block.
func (w *webCLI) startFailure(port int, err error) webFailure {
	text := strings.TrimSpace(err.Error())
	if strings.Contains(text, "daemon did not start") {
		return webFailure{
			process: "daemon",
			detail:  "the bonsai background daemon did not start",
			fix:     "run it in the foreground to see why:  bonsai __daemon --home " + w.home,
		}
	}
	name, rest, scoped := strings.Cut(text, ": ")
	if !scoped || name != "api" {
		return webFailure{detail: text, fix: "bonsai web --attach   to watch it start", logs: true}
	}
	lines := strings.Split(rest, "\n")
	head := strings.TrimSpace(lines[0])
	var tail []string
	for _, line := range lines[1:] {
		if _, marker := procstore.ParseMarker(line); marker {
			continue // lifecycle markers ("exited 1 · failed") repeat head
		}
		if line = strings.TrimSpace(line); line != "" {
			tail = append(tail, strings.TrimPrefix(line, "bonsai: "))
		}
	}
	detail := head
	if m := exitCodePattern.FindStringSubmatch(head); m != nil {
		detail = "exited with code " + m[1]
	} else if strings.HasPrefix(head, "readiness timed out") {
		detail = fmt.Sprintf("did not start listening on 127.0.0.1:%d in time", port)
	}
	if len(tail) > 0 {
		detail += " — " + tail[len(tail)-1]
	}
	return webFailure{
		process: "api",
		detail:  detail,
		fix:     "read the full output with the command below, fix the cause, then run bonsai web again",
		logs:    true,
	}
}
