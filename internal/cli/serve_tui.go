package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
)

type serveLogMsg string

type serveLogDoneMsg struct {
	err error
}

type serveTickMsg time.Time

type serveStatusMsg struct {
	group *procstore.ServeGroup
	err   error
}

type serveActionMsg struct {
	group *procstore.ServeGroup
	text  string
	err   error
}

type serveStoppedMsg struct {
	err error
}

type serveTUIModel struct {
	client  *client.Client
	groupID string
	group   *procstore.ServeGroup

	cancel context.CancelFunc
	events <-chan tea.Msg

	lines     []string
	selected  int
	search    string
	searching bool
	help      bool
	status    string
	width     int
	height    int
	logClosed bool
}

func runServeTUI(in io.Reader, out io.Writer, c *client.Client, group *procstore.ServeGroup) error {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan tea.Msg, 128)
	go func() {
		err := c.ServeLogs(ctx, group.ID, "", true, 500, "", false, func(chunk string) error {
			select {
			case events <- serveLogMsg(chunk):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		select {
		case events <- serveLogDoneMsg{err: err}:
		case <-ctx.Done():
		}
	}()

	model := serveTUIModel{
		client:   c,
		groupID:  group.ID,
		group:    group,
		cancel:   cancel,
		events:   events,
		selected: -1,
		width:    100,
		height:   30,
	}
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInput(in), tea.WithOutput(out))
	_, err := program.Run()
	cancel()
	return err
}

func (m serveTUIModel) Init() tea.Cmd {
	return tea.Batch(waitServeEvent(m.events), serveTick())
}

func waitServeEvent(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return serveLogDoneMsg{}
		}
		return msg
	}
}

func serveTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return serveTickMsg(t) })
}

func serveStatusCmd(c *client.Client, id string) tea.Cmd {
	return func() tea.Msg {
		group, err := c.ServeStatus(id)
		return serveStatusMsg{group: group, err: err}
	}
}

func serveRestartCmd(c *client.Client, id, process string) tea.Cmd {
	return func() tea.Msg {
		group, err := c.ServeRestart(id, process)
		text := "restarted serve group"
		if process != "" {
			text = "restarted " + process
		}
		return serveActionMsg{group: group, text: text, err: err}
	}
}

func serveStopCmd(c *client.Client, id string) tea.Cmd {
	return func() tea.Msg { return serveStoppedMsg{err: c.ServeStop(id)} }
}

func (m serveTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case serveLogMsg:
		m.appendLogChunk(string(msg))
		if !m.logClosed {
			return m, waitServeEvent(m.events)
		}
		return m, nil

	case serveLogDoneMsg:
		m.logClosed = true
		if msg.err != nil && !strings.Contains(msg.err.Error(), "context canceled") {
			m.status = "log stream: " + msg.err.Error()
		}
		return m, nil

	case serveTickMsg:
		return m, tea.Batch(serveStatusCmd(m.client, m.groupID), serveTick())

	case serveStatusMsg:
		if msg.err != nil {
			m.status = "status: " + msg.err.Error()
		} else if msg.group != nil {
			m.group = msg.group
			if m.selected >= len(m.group.Processes) {
				m.selected = -1
			}
		}
		return m, nil

	case serveActionMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
		} else {
			m.group = msg.group
			m.status = msg.text
		}
		return m, nil

	case serveStoppedMsg:
		if msg.err != nil {
			m.status = "stop: " + msg.err.Error()
			return m, nil
		}
		m.cancel()
		return m, tea.Quit

	case tea.KeyMsg:
		if m.searching {
			switch msg.Type {
			case tea.KeyEsc:
				m.searching = false
			case tea.KeyEnter:
				m.searching = false
			case tea.KeyBackspace, tea.KeyDelete:
				runes := []rune(m.search)
				if len(runes) > 0 {
					m.search = string(runes[:len(runes)-1])
				}
			case tea.KeyRunes:
				m.search += string(msg.Runes)
			}
			return m, nil
		}

		key := msg.String()
		switch key {
		case "q", "ctrl+c":
			m.cancel()
			return m, tea.Quit
		case "X":
			m.status = "stopping serve group..."
			return m, serveStopCmd(m.client, m.groupID)
		case "R":
			m.status = "restarting serve group..."
			return m, serveRestartCmd(m.client, m.groupID, "")
		case "r":
			name := m.selectedProcess()
			if name == "" {
				m.status = "select a process before using r"
				return m, nil
			}
			m.status = "restarting " + name + "..."
			return m, serveRestartCmd(m.client, m.groupID, name)
		case "a":
			m.selected = -1
		case "f":
			if len(m.group.Processes) > 0 {
				m.selected++
				if m.selected >= len(m.group.Processes) {
					m.selected = -1
				}
			}
		case "/":
			m.searching = true
		case "?":
			m.help = !m.help
		default:
			if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
				index, _ := strconv.Atoi(key)
				index--
				if index >= 0 && index < len(m.group.Processes) {
					m.selected = index
				}
			}
		}
	}
	return m, nil
}

func (m *serveTUIModel) appendLogChunk(chunk string) {
	for _, line := range strings.Split(chunk, "\n") {
		if line == "" {
			continue
		}
		m.lines = append(m.lines, line)
	}
	if len(m.lines) > 4000 {
		m.lines = append([]string(nil), m.lines[len(m.lines)-4000:]...)
	}
}

func (m serveTUIModel) selectedProcess() string {
	if m.selected < 0 || m.selected >= len(m.group.Processes) {
		return ""
	}
	return m.group.Processes[m.selected].Name
}

func (m serveTUIModel) visibleLines() []string {
	name := m.selectedProcess()
	search := strings.ToLower(m.search)
	out := make([]string, 0, len(m.lines))
	for _, line := range m.lines {
		if name != "" && !strings.Contains(line, "] "+name+" ") {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(line), search) {
			continue
		}
		out = append(out, line)
	}
	maxLines := m.height - len(m.group.Processes) - 10
	if maxLines < 5 {
		maxLines = 5
	}
	if len(out) > maxLines {
		out = out[len(out)-maxLines:]
	}
	return out
}

func (m serveTUIModel) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Bonsai Serve — %s\n\n", m.group.WorkspacePath)
	for i, process := range m.group.Processes {
		marker := " "
		if i == m.selected {
			marker = ">"
		}
		address := ""
		if process.ExpectedPort > 0 {
			address = " 127.0.0.1:" + strconv.Itoa(process.ExpectedPort)
		} else if process.PID > 0 {
			address = " pid " + strconv.Itoa(process.PID)
		}
		fmt.Fprintf(&b, "%s %-10s %s%s\n", marker, process.Name, serveStateLabel(process.State), address)
	}
	b.WriteString("\n")
	filter := "ALL"
	if name := m.selectedProcess(); name != "" {
		filter = name
	}
	fmt.Fprintf(&b, "Logs [%s]", filter)
	if m.search != "" || m.searching {
		fmt.Fprintf(&b, "  search: %s", m.search)
		if m.searching {
			b.WriteString("█")
		}
	}
	b.WriteString("\n")
	width := m.width
	if width < 20 {
		width = 20
	}
	b.WriteString(strings.Repeat("─", min(width, 100)))
	b.WriteString("\n")
	for _, line := range m.visibleLines() {
		if len([]rune(line)) > width {
			runes := []rune(line)
			line = string(runes[:width])
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString(strings.Repeat("─", min(width, 100)))
	b.WriteString("\n")
	if m.help {
		b.WriteString("a all · 1-9 select · f cycle · / search · r restart selected · R restart all · q/Ctrl+C detach · X stop all · ? help\n")
	} else {
		b.WriteString("ALL  / search  f filter  r restart  q detach  X stop  ? help\n")
	}
	if m.status != "" {
		b.WriteString(m.status)
		b.WriteByte('\n')
	}
	return b.String()
}

func serveStateLabel(state string) string {
	switch state {
	case "ready", "running":
		return "● " + state
	case procstore.StatusStarting, procstore.StatusBackoff:
		return "◐ " + state
	default:
		return "○ " + state
	}
}
