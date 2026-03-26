package ui

import (
	"fmt"
	"strings"

	"github.com/adamarutyunov/launch/internal/process"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// startDialogState holds the process that triggered the dependency dialog and
// its full transitive dependency tree (used for "start with all deps" action).
type startDialogState struct {
	process        *process.ManagedProcess
	dependencyTree []*process.ManagedProcess
}

// showStartDialogMsg is sent (via Notifier) when a process cannot be started
// because its dependencies are not running and the user should choose an action.
type showStartDialogMsg struct {
	process *process.ManagedProcess
}

// handleDialogKey processes a key press while the start dialog is visible.
// Returns (model, cmd, handled). If handled is false the key should fall
// through to the normal Update handler.
func (m *Model) handleDialogKey(key string) (tea.Model, tea.Cmd, bool) {
	if m.startDialog == nil {
		return m, nil, false
	}

	dialog := m.startDialog
	manager := m.manager

	switch key {
	case "r", "esc":
		m.startDialog = nil
		m.updateViewportContent()
		return m, nil, true

	case "d":
		m.startDialog = nil
		m.updateViewportContent()
		return m, func() tea.Msg {
			manager.StartBatch(dialog.dependencyTree)
			return nil
		}, true

	case "f":
		m.startDialog = nil
		m.updateViewportContent()
		return m, func() tea.Msg {
			_ = dialog.process.Start()
			return nil
		}, true
	}

	// Block all other keys while dialog is open.
	return m, nil, true
}

func (m Model) renderStartDialog() string {
	dialog := m.startDialog
	proc := dialog.process
	unmet := m.manager.CheckDependencies(proc)

	header := logHeaderStyle.Render(fmt.Sprintf("Start %q?", proc.Title))

	var body strings.Builder
	body.WriteString("  Dependencies not running:\n")
	for _, dep := range unmet {
		body.WriteString(fmt.Sprintf("    ○ %s\n", dep))
	}
	body.WriteString("\n")

	options := helpStyle.Render("  [d] start with all dependencies   [f] force start (skip deps)   [r/esc] cancel")

	return lipgloss.JoinVertical(lipgloss.Left, header, body.String(), options)
}
