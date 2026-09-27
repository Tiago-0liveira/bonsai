// Package pkgmgr discovers project/build commands from package managers and build tools.
package pkgmgr

import (
	"sort"
	"strings"
)

// PackageManager is the temporary compatibility surface used by the scripts modal.
type PackageManager interface {
	Name() string
	GetScripts() []string
	RunCommand(name string) string
}

type compatibilityManager struct {
	name     string
	commands map[string]Command
}

func (m *compatibilityManager) Name() string { return m.name }

func (m *compatibilityManager) GetScripts() []string {
	names := make([]string, 0, len(m.commands))
	for name := range m.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (m *compatibilityManager) RunCommand(name string) string {
	cmd, ok := m.commands[name]
	if !ok {
		return ""
	}
	parts := append([]string{cmd.Invocation.Program}, cmd.Invocation.Prefix...)
	return strings.Join(parts, " ")
}

// Detect adapts the discovery engine to the legacy single-provider API.
// The TUI now consumes Discover directly, but this adapter remains for callers
// that still expect one package/build manager.
func Detect(dir string) (PackageManager, error) {
	return DetectWithOptions(dir, Options{UseCache: true})
}

// DetectWithOptions adapts discovery to the legacy scripts modal while allowing
// callers to configure bounded manifest search.
func DetectWithOptions(dir string, opts Options) (PackageManager, error) {
	project, err := Discover(dir, opts)
	if err != nil {
		return nil, err
	}
	for _, wanted := range []string{"node:", "python:", "go", "cargo", "make"} {
		for _, provider := range project.Providers {
			match := provider.ID == wanted || (strings.HasSuffix(wanted, ":") && strings.HasPrefix(provider.ID, wanted))
			if !match {
				continue
			}
			commands := map[string]Command{}
			for _, cmd := range project.Commands {
				if cmd.Provider == provider.ID {
					commands[cmd.Name] = cmd
				}
			}
			if provider.ID == "make" && len(commands) == 0 {
				continue
			}
			return &compatibilityManager{name: provider.Name, commands: commands}, nil
		}
	}
	return nil, nil
}
