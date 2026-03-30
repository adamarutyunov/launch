// Package embed exposes launch's process manager and UI as a composable
// BubbleTea model so it can be hosted inside another TUI (e.g. squirrel).
//
// Usage:
//
//	panel, err := embed.New(contextPath)
//	// Wire into parent model:
//	initCmd := panel.Init()
//	// In parent Update:
//	case embed.EventMsg:
//	    newPanel, cmd := panel.Update(msg)
//	    panel = newPanel
//	// In parent View (right panel):
//	return panel.View()
package embed

import (
	"github.com/adamarutyunov/launch/internal/config"
	"github.com/adamarutyunov/launch/internal/process"
	"github.com/adamarutyunov/launch/internal/state"
	"github.com/adamarutyunov/launch/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

// EventMsg carries a process event (LogMsg, StatusChangeMsg, AlertMsg) from
// the background process goroutines into the parent BubbleTea event loop.
// The parent must route this back to Model.Update so launch handles it.
type EventMsg struct {
	Tag   int // Caller-defined tag for routing events to the correct panel.
	inner any
}

// Model is a self-contained, embeddable launch panel.
// It is not a tea.Model itself — the parent drives it by calling Init/Update/View.
type Model struct {
	Tag     int // Caller-defined tag, propagated into EventMsg for routing.
	inner   ui.Model
	manager *process.Manager
	eventCh chan any
}

// New creates a Model for the given root directory by discovering launch.yml
// and Taskfile.yml files. Returns an error if discovery fails.
// Call HasProcesses() to check whether anything was found before displaying.
func New(rootDir string) (Model, error) {
	groups, err := config.Discover(rootDir)
	if err != nil {
		return Model{}, err
	}

	eventCh := make(chan any, 256)
	manager := process.NewManager(rootDir)

	for _, group := range groups {
		for _, namedProc := range group.Processes {
			workingDir := ""
			if namedProc.Process.WorkingDir != nil {
				workingDir = *namedProc.Process.WorkingDir
			}
			logFile := state.LogFilePath(rootDir, group.Name, namedProc.Slug)
			managed := process.NewManagedProcess(
				namedProc.Slug,
				namedProc.Process.Title,
				group.Name,
				namedProc.Process.Command,
				workingDir,
				namedProc.Process.Env,
				namedProc.Process.AutoStart,
				namedProc.Process.DependsOn,
				namedProc.Process.ReadyCheck,
				logFile,
			)
			manager.Add(managed)
		}
		for _, namedTask := range group.Tasks {
			managed := process.NewManagedTask(
				namedTask.Slug,
				namedTask.Desc,
				group.Name,
				namedTask.Command,
				namedTask.WorkingDir,
			)
			manager.AddTask(managed)
		}
	}

	manager.SetNotifier(chanNotifier{ch: eventCh})

	settings := state.LoadSettings(rootDir)
	inner := ui.NewModel(manager, "Launch", settings)
	inner.NoAutoStart = true

	if session, err := state.Load(rootDir); err == nil && len(session.Processes) > 0 {
		inner.SavedSession = session
	}

	return Model{inner: inner, manager: manager, eventCh: eventCh}, nil
}

// HasProcesses reports whether any processes or tasks were discovered.
func (m Model) HasProcesses() bool {
	return len(m.manager.Items) > 0
}

// StartAutoStart starts all auto_start processes using dependency-ordered batch launch.
func (m Model) StartAutoStart() { m.manager.StartAutoStart() }

// ForceStartAutoStart starts all auto_start processes individually, skipping dependency checks.
func (m Model) ForceStartAutoStart() {
	for _, proc := range m.manager.Processes {
		if proc.AutoStart && !proc.Status().IsUp() {
			proc.Start()
		}
	}
}

// StopAll stops all running processes without saving state.
func (m Model) StopAll() { m.manager.StopAll() }

// SaveState persists the session so processes survive a detach.
func (m Model) SaveState() error { return m.manager.SaveState() }

// Init returns the commands that launch needs to run on startup.
// The parent should batch this with its own Init commands.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.inner.Init(), m.waitForEventCmd())
}

// Update handles a message. The parent should call this for:
//   - All embed.EventMsg messages (always)
//   - tea.WindowSizeMsg (always, so layout stays correct)
//   - tea.KeyMsg when the launch pane has focus
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case EventMsg:
		// A process event arrived via the channel — forward to inner model.
		newInner, cmd := m.inner.Update(msg.inner)
		if inner, ok := newInner.(ui.Model); ok {
			m.inner = inner
		}
		return m, tea.Batch(cmd, m.waitForEventCmd())
	default:
		newInner, cmd := m.inner.Update(msg)
		if inner, ok := newInner.(ui.Model); ok {
			m.inner = inner
		}
		return m, cmd
	}
}

// View renders the launch panel. The parent places this string in its right panel.
func (m Model) View() string { return m.inner.View() }

// waitForEventCmd blocks until a process event arrives on the channel,
// then returns it as an EventMsg for the parent's BubbleTea loop.
func (m Model) waitForEventCmd() tea.Cmd {
	return func() tea.Msg {
		return EventMsg{Tag: m.Tag, inner: <-m.eventCh}
	}
}

// chanNotifier satisfies process.Notifier using a buffered channel.
// Drops events if the channel is full to avoid blocking process goroutines.
type chanNotifier struct{ ch chan any }

func (n chanNotifier) Send(msg any) {
	select {
	case n.ch <- msg:
	default:
	}
}
