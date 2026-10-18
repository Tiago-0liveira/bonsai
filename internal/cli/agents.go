package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"golang.org/x/term"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

const agentsUsage = `usage: bonsai agents [ls] [--all] [--json]
       bonsai agents attach <id|name> [--take] [--read-only]
       bonsai agents stop <id|name>`

// escapeKey (Ctrl-]) starts an attach command: d detaches, k asks to kill, t takes control,
// a second Ctrl-] sends one to the agent.
const escapeKey = 0x1d

// agentsCLI is one `bonsai agents` invocation. It reads the agent holders'
// records and talks to their sockets directly, so it works from any
// directory whether or not bonsai web runs.
type agentsCLI struct {
	paths       agenthost.Paths
	in          io.Reader
	out, errOut io.Writer
	now         func() time.Time
}

func cmdAgents(args []string, in io.Reader, out, errOut io.Writer) error {
	paths, err := agenthost.DefaultPaths()
	if err != nil {
		return fmt.Errorf("cannot locate agent sessions: %w", err)
	}
	c := &agentsCLI{paths: paths, in: in, out: out, errOut: errOut, now: time.Now}
	return c.run(args)
}

func (c *agentsCLI) run(args []string) error {
	sub := "ls"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "ls", "list":
		return c.list(args)
	case "attach":
		return c.attach(args)
	case "stop":
		return c.stop(args)
	case "help":
		fmt.Fprintln(c.out, agentsUsage)
		return nil
	}
	return fmt.Errorf("unknown bonsai agents command %q\n%s", sub, agentsUsage)
}

// parseAgentArgs splits flags (any position) from at most one argument.
func parseAgentArgs(args []string, flags map[string]*bool) (string, error) {
	target := ""
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flag, known := flags[strings.TrimLeft(arg, "-")]
			if !known {
				return "", fmt.Errorf("unknown flag %s\n%s", arg, agentsUsage)
			}
			*flag = true
			continue
		}
		if target != "" {
			return "", fmt.Errorf("%s", agentsUsage)
		}
		target = arg
	}
	return target, nil
}

// agentJSON is one session in `bonsai agents --json`.
type agentJSON struct {
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	Provider     string                       `json:"provider"`
	Profile      string                       `json:"profile"`
	ProjectID    string                       `json:"project_id"`
	WorktreeID   string                       `json:"worktree_id"`
	WorkDir      string                       `json:"work_dir"`
	State        string                       `json:"state"`
	Alive        bool                         `json:"alive"`
	PID          int                          `json:"pid,omitempty"`
	ExitCode     *int                         `json:"exit_code,omitempty"`
	Error        string                       `json:"error,omitempty"`
	CreatedAt    time.Time                    `json:"created_at"`
	StartedAt    *time.Time                   `json:"started_at,omitempty"`
	EndedAt      *time.Time                   `json:"ended_at,omitempty"`
	LastOutputAt *time.Time                   `json:"last_output_at,omitempty"`
	Clients      []agentterminal.StreamClient `json:"clients"`
}

func (c *agentsCLI) list(args []string) error {
	var all, asJSON bool
	if target, err := parseAgentArgs(args, map[string]*bool{"all": &all, "a": &all, "json": &asJSON}); err != nil || target != "" {
		if err == nil {
			err = fmt.Errorf("%s", agentsUsage)
		}
		return err
	}
	rows, err := agentRows(c.paths, all)
	if err != nil {
		return err
	}
	if asJSON {
		out := []agentJSON{}
		for _, row := range rows {
			s := row.Summary
			item := agentJSON{ID: row.ID, Name: s.Name, Provider: string(s.Provider), Profile: s.ProfileName, ProjectID: s.ProjectID, WorktreeID: s.WorktreeID, WorkDir: row.WorkDir, State: s.State, Alive: row.Alive, ExitCode: s.ExitCode, Error: s.Error, CreatedAt: s.CreatedAt, StartedAt: s.StartedAt, EndedAt: s.EndedAt, LastOutputAt: row.LastOutputAt, Clients: []agentterminal.StreamClient{}}
			if row.Alive {
				item.PID = row.PID
			}
			if row.info != nil {
				item.LastOutputAt, item.Clients = row.info.LastOutputAt, row.info.Clients
			}
			out = append(out, item)
		}
		encoder := json.NewEncoder(c.out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(out)
	}
	if len(rows) == 0 {
		if all {
			fmt.Fprintln(c.out, "No agent sessions.")
		} else {
			fmt.Fprintln(c.out, "No agents running. `bonsai agents --all` includes ended ones.")
		}
		return nil
	}
	return writeAgentTable(c.out, rows, c.now())
}

// resolve finds one session by ID, ID prefix (with or without sess_) or name.
// When a name or prefix matches several, a single running one wins.
func (c *agentsCLI) resolve(query string) (agentRow, error) {
	if query == "" {
		return agentRow{}, fmt.Errorf("%s", agentsUsage)
	}
	rows, err := agentRows(c.paths, true)
	if err != nil {
		return agentRow{}, err
	}
	prefix := strings.TrimPrefix(query, "sess_")
	var matches []agentRow
	for _, row := range rows {
		if row.ID == query {
			return row, nil
		}
		if strings.HasPrefix(strings.TrimPrefix(row.ID, "sess_"), prefix) || strings.EqualFold(row.Summary.Name, query) {
			matches = append(matches, row)
		}
	}
	if len(matches) > 1 {
		var live []agentRow
		for _, row := range matches {
			if row.Alive && row.Summary.Active() {
				live = append(live, row)
			}
		}
		if len(live) == 1 {
			return live[0], nil
		}
		sort.Slice(matches, func(i, j int) bool { return matches[i].Summary.CreatedAt.After(matches[j].Summary.CreatedAt) })
		var b strings.Builder
		fmt.Fprintf(&b, "%q matches %d agent sessions; use a longer ID:", query, len(matches))
		for _, row := range matches {
			fmt.Fprintf(&b, "\n  %s  %s  %s", strings.TrimPrefix(row.ID, "sess_"), row.Summary.Name, agentState(row))
		}
		return agentRow{}, errors.New(b.String())
	}
	if len(matches) == 0 {
		return agentRow{}, fmt.Errorf("no agent session %q; `bonsai agents --all` lists them", query)
	}
	return matches[0], nil
}

func (c *agentsCLI) stop(args []string) error {
	target, err := parseAgentArgs(args, map[string]*bool{})
	if err != nil {
		return err
	}
	row, err := c.resolve(target)
	if err != nil {
		return err
	}
	if !row.Alive || !row.Summary.Active() {
		fmt.Fprintf(c.out, "%s already ended (%s)\n", row.Summary.Name, agentState(row))
		return nil
	}
	if err := agenthost.StopHolder(c.paths, row.Record, ""); err != nil {
		return fmt.Errorf("stop %s: %w", row.Summary.Name, err)
	}
	if !agenthost.Wait(c.paths, row.ID, 15*time.Second) {
		fmt.Fprintf(c.out, "stop requested; %s is still finishing\n", row.Summary.Name)
		return nil
	}
	fmt.Fprintf(c.out, "✓ stopped %s\n", row.Summary.Name)
	return nil
}

func (c *agentsCLI) attach(args []string) error {
	var opts attachOptions
	target, err := parseAgentArgs(args, map[string]*bool{"take": &opts.take, "read-only": &opts.readOnly})
	if err != nil {
		return err
	}
	if opts.take && opts.readOnly {
		return errors.New("--take and --read-only exclude each other")
	}
	row, err := c.resolve(target)
	if err != nil {
		return err
	}
	if !row.Alive || !row.Summary.Active() {
		return fmt.Errorf("%s has ended (%s); `bonsai agents --all` lists ended sessions", row.Summary.Name, agentState(row))
	}
	stdin, isFile := c.in.(*os.File)
	tty := isFile && term.IsTerminal(int(stdin.Fd()))
	if !tty && !opts.readOnly {
		return errors.New("attach needs a terminal; pass --read-only to watch the output")
	}
	size := func() (int, int, bool) { return 0, 0, false }
	if stdout, ok := c.out.(*os.File); ok && term.IsTerminal(int(stdout.Fd())) {
		size = func() (int, int, bool) {
			cols, rows, err := term.GetSize(int(stdout.Fd()))
			return cols, rows, err == nil && agentterminal.Dimensions(cols, rows)
		}
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(quit)
	if tty {
		state, err := term.MakeRaw(int(stdin.Fd()))
		if err != nil {
			return err
		}
		defer func() { _ = term.Restore(int(stdin.Fd()), state) }()
	} else {
		// Without raw mode, Ctrl-C must still end the watch, not the terminal.
		signal.Notify(quit, os.Interrupt)
	}
	resize := make(chan os.Signal, 1)
	notifyResize(resize)
	defer signal.Stop(resize)
	return attachTerminal(row.Record, row.Summary.Name, opts, c.in, c.out, c.errOut, size, resize, quit)
}

type attachOptions struct{ take, readOnly bool }

// attachTerminal runs one attachment: the replay and live output go to out,
// keys from in go to the agent while this client writes, and status lines go
// to errOut. Ctrl-] then d detaches, t takes control. It returns when the
// client detaches, the session ends, quit fires or the holder disconnects.
func attachTerminal(r agenthost.Record, name string, opts attachOptions, in io.Reader, out, errOut io.Writer, size func() (int, int, bool), resize, quit <-chan os.Signal) error {
	conn, ready, err := agenthost.Dial(r, agenthost.Request{Mode: agenthost.ModeTerminal, Client: "cli", Take: opts.take, ReadOnly: opts.readOnly})
	if errors.Is(err, agenthost.ErrVersion) {
		return fmt.Errorf("%s was started by another bonsai version; stop it with `bonsai agents stop %s` or let it finish", name, shortAgentID(r.ID))
	}
	if err != nil {
		return fmt.Errorf("attach %s: %w", name, err)
	}
	defer conn.Close()
	writer := ready.Writer != nil && *ready.Writer
	var display *attachDisplay
	var terminalInput <-chan []byte
	var lastStatus string
	if cols, rows, ok := size(); ok {
		display = newAttachDisplay(out, cols, rows, opts.readOnly, writer)
		if !writer {
			// Replay must use the holder's geometry even when this observer's
			// viewport is smaller. Observing never resizes the agent.
			if info, err := agenthost.Query(r); err == nil {
				display.term.ResizeEmulator(info.Cols, info.Rows)
			}
		}
		terminalInput = display.term.Input()
		_ = conn.Send(agenthost.Request{Type: "info"})
		defer func() {
			display.close()
			fmt.Fprintf(errOut, "\r\n[bonsai] %s\r\n", lastStatus)
		}()
	}
	say := func(format string, args ...any) {
		lastStatus = fmt.Sprintf(format, args...)
		if display != nil {
			display.status, display.dirty = lastStatus, true
			display.render()
		} else {
			fmt.Fprintf(errOut, "\r\n[bonsai] %s\r\n", lastStatus)
		}
	}
	sendSize := func(nudge bool) {
		cols, rows, ok := size()
		if !ok {
			return
		}
		if display != nil {
			display.writer = writer
			display.resize(cols, rows)
			rows = display.bodyRows()
		}
		// Shrinking first makes a full-screen program redraw even when the
		// size is unchanged.
		if nudge && rows > 1 {
			_ = conn.Send(agenthost.Request{Type: "resize", Cols: cols, Rows: rows - 1})
		}
		_ = conn.Send(agenthost.Request{Type: "resize", Cols: cols, Rows: rows})
	}
	switch {
	case opts.readOnly:
		say("watching %s (read-only; Ctrl-] d to detach, Ctrl-] k to kill)", name)
	case writer:
		say("attached to %s (Ctrl-] d to detach, Ctrl-] k to kill)", name)
		sendSize(true)
	default:
		say("attached to %s (read-only — Ctrl-] t to take control, Ctrl-] d to detach, Ctrl-] k to kill)", name)
	}

	done := make(chan struct{})
	defer close(done)
	frames := make(chan agenthost.Frame, 64)
	go func() {
		defer close(frames)
		for {
			f, err := conn.Recv()
			if err != nil {
				return
			}
			select {
			case frames <- f:
			case <-done:
				return
			}
		}
	}()
	var keys <-chan []byte
	var events <-chan uv.Event
	if display != nil {
		var cleanup func()
		events, cleanup, err = attachEvents(in)
		if err != nil {
			return err
		}
		defer cleanup()
	} else {
		raw := make(chan []byte)
		keys = raw
		go func() {
			defer close(raw)
			buf := make([]byte, 4096)
			for {
				n, err := in.Read(buf)
				if n > 0 {
					select {
					case raw <- append([]byte(nil), buf[:n]...):
					case <-done:
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()
	}

	escaped, hinted, askedWho, confirming, stopping := false, false, false, false, false
	handleInput := func(data []byte) (bool, error) {
		var send []byte
		for _, b := range data {
			if confirming {
				confirming = false
				if display != nil {
					display.confirm = false
					display.term.SetFocused(writer)
				}
				if b == 'y' || b == 'Y' {
					if err := conn.Send(agenthost.Request{Type: "stop"}); err != nil {
						return true, fmt.Errorf("stop %s: %w", name, err)
					}
					stopping = true
					say("stopping %s…", name)
				} else {
					say("kill cancelled; %s keeps running", name)
				}
				// Do not forward the answer or the rest of a key sequence
				// (such as an arrow key) into the agent.
				break
			}
			if escaped {
				escaped = false
				switch b {
				case 'd', 'D':
					say("detached from %s; it keeps running", name)
					return true, nil
				case 't', 'T':
					if opts.readOnly {
						say("started with --read-only; attach again to take control")
					} else if !writer {
						_ = conn.Send(agenthost.Request{Type: "take"})
					}
				case 'k', 'K':
					confirming = true
					if display != nil {
						display.confirm = true
						display.term.SetFocused(false)
					}
					say("kill %s? [y/N] (Esc cancels)", name)
					send = nil
				case escapeKey:
					send = append(send, escapeKey)
				}
				continue
			}
			if b == escapeKey {
				escaped = true
				continue
			}
			send = append(send, b)
		}
		if len(send) == 0 {
			return false, nil
		}
		if stopping || confirming {
			return false, nil
		}
		if !writer {
			if !hinted && !opts.readOnly {
				hinted = true
				say("read-only — Ctrl-] t to take control")
			}
			return false, nil
		}
		for len(send) > 0 {
			n := min(len(send), agentterminal.InputLimit)
			_ = conn.Send(agenthost.Request{Type: "input", Data: send[:n]})
			send = send[n:]
		}
		return false, nil
	}
	tick := time.NewTicker(time.Second / 30)
	defer tick.Stop()
	observeTick := time.NewTicker(time.Second)
	defer observeTick.Stop()
	for {
		select {
		case <-observeTick.C:
			if display != nil && !writer {
				_ = conn.Send(agenthost.Request{Type: "info"})
			}
		case <-tick.C:
			if display != nil {
				display.render()
			}
		case data, ok := <-terminalInput:
			if !ok {
				terminalInput = nil
			} else if writer && !confirming && !stopping {
				_ = conn.Send(agenthost.Request{Type: "input", Data: data})
			}
		case <-quit:
			say("detached from %s", name)
			return nil
		case <-resize:
			if display != nil {
				if cols, rows, ok := size(); ok {
					display.resize(cols, rows)
				}
				_ = conn.Send(agenthost.Request{Type: "info"})
			}
			if writer {
				sendSize(false)
			}
		case f, ok := <-frames:
			if !ok {
				say("%s: agent host closed the connection", name)
				return nil
			}
			switch f.Type {
			case "output":
				if display == nil {
					_, _ = out.Write(f.Data)
				} else {
					display.term.Write(f.Data)
					display.dirty = true
				}
			case "writer":
				writer = f.Writer != nil && *f.Writer
				if display != nil {
					display.writer, display.dirty = writer, true
					display.term.SetFocused(writer && !confirming)
				}
				if writer {
					say("you have control of %s", name)
					sendSize(true)
				} else {
					askedWho = true
					_ = conn.Send(agenthost.Request{Type: "info"})
				}
			case "info":
				if display != nil && !writer && f.Info != nil {
					display.term.ResizeEmulator(f.Info.Cols, f.Info.Rows)
					display.dirty = true
				}
				if askedWho && f.Info != nil {
					askedWho = false
					by := "another terminal"
					for _, client := range f.Info.Clients {
						if client.Writer && client.Client != "" {
							by = client.Client
						}
					}
					say("control taken by %s (Ctrl-] t to take it back)", by)
				}
			case "status":
				if display != nil {
					display.term.SetReplaying(false)
				}
				if f.Status != nil && !f.Status.Active() {
					switch {
					case f.Status.State == "failed" && f.Status.Error != "":
						say("agent failed: %s", f.Status.Error)
					case f.Status.ExitCode != nil:
						say("agent exited (code %d)", *f.Status.ExitCode)
					default:
						say("agent %s", f.Status.State)
					}
					return nil
				}
			}
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if k, ok := event.(uv.KeyPressEvent); ok {
				var command []byte
				if k.MatchString("ctrl+]") {
					command = []byte{escapeKey}
				} else if escaped || confirming {
					command = []byte{0}
					if text := k.String(); len(text) == 1 {
						command = []byte(text)
					}
				}
				if command != nil {
					if finished, err := handleInput(command); finished {
						return err
					}
					continue
				}
			}
			if writer && !escaped && !confirming && !stopping {
				display.term.SendEvent(event)
			} else if !confirming && !stopping {
				if finished, err := handleInput([]byte{0}); finished {
					return err
				}
			}
		case data, ok := <-keys:
			if !ok {
				keys = nil // Input EOF keeps the output watch open.
				continue
			}
			if finished, err := handleInput(data); finished {
				return err
			}
		}
	}
}
