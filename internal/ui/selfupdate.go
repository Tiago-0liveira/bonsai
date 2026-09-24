package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/updater"
	"github.com/Tiago-0liveira/bonsai/internal/version"
)

type updateAvailableMsg struct{ release updater.Release }
type updateInstalledMsg struct {
	err error
	tag string
}

func checkForUpdate() tea.Msg {
	if version.Version == "dev" {
		return updateAvailableMsg{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := updater.New().CachedLatest(ctx)
	if err != nil || !updater.Newer(r.Tag, version.String()) {
		return updateAvailableMsg{}
	}
	return updateAvailableMsg{r}
}
func installUpdate(r updater.Release) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return updateInstalledMsg{updater.New().Install(ctx, r), r.Tag}
	}
}
func (m Model) updatePromptVisible() bool {
	return m.availableUpdate.Tag != "" && m.modal == nil && m.prefs == nil && m.ready
}
func (m Model) updatePrompt() string {
	if m.updating {
		return "Updating Bonsai…\n\n[l] Continue working"
	}
	return fmt.Sprintf("Bonsai %s is available.\nCurrent: %s\n\n[u] Update\n[l] Later", m.availableUpdate.Tag, version.String())
}
