package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/adam/launch/internal/process"
	"github.com/adam/launch/internal/state"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	sidebarWidth = 32

	colorRunning  = lipgloss.Color("#22c55e")
	colorStarting = lipgloss.Color("#f59e0b")
	colorQueued   = lipgloss.Color("#a1a1aa")
	colorStopped  = lipgloss.Color("#71717a")
	colorCrashed  = lipgloss.Color("#ef4444")
	colorDim      = lipgloss.Color("#52525b")
	colorBorder   = lipgloss.Color("#3f3f46")
	colorGroup    = lipgloss.Color("#a1a1aa")
	colorAlert    = lipgloss.Color("#f59e0b")

	sidebarStyle = lipgloss.NewStyle().
			Width(sidebarWidth).
			BorderRight(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Padding(1, 0)

	selectedItemStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#27272a")).
				Width(sidebarWidth - 2).
				Padding(0, 1)

	normalItemStyle = lipgloss.NewStyle().
			Width(sidebarWidth - 2).
			Padding(0, 1)

	groupHeaderStyle = lipgloss.NewStyle().
				Foreground(colorGroup).
				Bold(true).
				Width(sidebarWidth - 2).
				Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			MarginBottom(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Padding(0, 1)

	logHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1)

	systemLogStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Italic(true)

	alertStyle = lipgloss.NewStyle().
			Foreground(colorAlert).
			Bold(true).
			Padding(0, 1)
)

type ExitMode int

const (
	ExitDetach ExitMode = iota
	ExitKill
)

type Model struct {
	manager           *process.Manager
	selectedIndex     int
	selectableIndices []int
	viewport          viewport.Model
	width, height     int
	ready             bool
	autoScroll        bool
	title             string
	multiGroup        bool
	ExitMode          ExitMode
	SavedSession      *state.SessionState
	collapsedGroups   map[string]bool
	alert             string
	alertExpiry       time.Time
}

func NewModel(manager *process.Manager, title string, settings *state.UserSettings) Model {
	collapsedGroups := settings.CollapsedGroups
	manager.BuildSidebar(collapsedGroups)
	selectable := manager.SelectableIndices()

	return Model{
		manager:           manager,
		selectedIndex:     0,
		selectableIndices: selectable,
		autoScroll:        true,
		title:             title,
		multiGroup:        len(manager.Groups) > 1,
		ExitMode:          ExitDetach,
		collapsedGroups:   collapsedGroups,
	}
}

func (m Model) selectedEntry() *process.SidebarEntry {
	if len(m.selectableIndices) == 0 {
		return nil
	}
	sidebarIdx := m.selectableIndices[m.selectedIndex]
	return &m.manager.Sidebar[sidebarIdx]
}

func (m Model) selectedProcess() *process.ManagedProcess {
	entry := m.selectedEntry()
	if entry == nil || entry.IsGroup {
		return nil
	}
	return entry.Process
}

func (m *Model) rebuildSidebar() {
	selectedGroup := ""
	if entry := m.selectedEntry(); entry != nil {
		selectedGroup = entry.Group
	}
	m.manager.BuildSidebar(m.collapsedGroups)
	m.selectableIndices = m.manager.SelectableIndices()

	// Try to select the group header after collapse
	if selectedGroup != "" {
		for i, idx := range m.selectableIndices {
			entry := m.manager.Sidebar[idx]
			if entry.IsGroup && entry.Group == selectedGroup {
				m.selectedIndex = i
				break
			}
		}
	}
	if m.selectedIndex >= len(m.selectableIndices) {
		m.selectedIndex = len(m.selectableIndices) - 1
	}
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}
}

func (m *Model) saveSettings() {
	go state.SaveSettings(m.manager.RootDir, &state.UserSettings{
		CollapsedGroups: m.collapsedGroups,
	})
}

func (m *Model) setAlert(text string) tea.Cmd {
	m.alert = text
	m.alertExpiry = time.Now().Add(5 * time.Second)
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearAlertMsg{}
	})
}

type clearAlertMsg struct{}

func (m Model) Init() tea.Cmd {
	return func() tea.Msg {
		if m.SavedSession != nil {
			m.manager.ReattachFromState(m.SavedSession)
		} else {
			m.manager.StartAutoStart()
		}
		return nil
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case clearAlertMsg:
		if time.Now().After(m.alertExpiry) {
			m.alert = ""
		}
		return m, nil

	case process.AlertMsg:
		cmd := m.setAlert(msg.Text)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		logWidth := m.width - sidebarWidth - 3
		logHeight := m.height - 4

		if !m.ready {
			m.viewport = viewport.New(logWidth, logHeight)
			m.viewport.Style = lipgloss.NewStyle()
			m.ready = true
		} else {
			m.viewport.Width = logWidth
			m.viewport.Height = logHeight
		}
		m.updateViewportContent()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			m.ExitMode = ExitDetach
			return m, tea.Quit

		case "Q", "ctrl+c":
			m.ExitMode = ExitKill
			return m, tea.Quit

		case "enter":
			if !m.multiGroup {
				return m, nil
			}
			entry := m.selectedEntry()
			if entry == nil || !entry.IsGroup {
				return m, nil
			}
			group := entry.Group
			m.collapsedGroups[group] = !m.collapsedGroups[group]
			m.rebuildSidebar()
			m.updateViewportContent()
			m.saveSettings()
			return m, nil

		case "j", "down":
			if m.selectedIndex < len(m.selectableIndices)-1 {
				m.selectedIndex++
				m.autoScroll = true
				m.updateViewportContent()
			}
			return m, nil

		case "k", "up":
			if m.selectedIndex > 0 {
				m.selectedIndex--
				m.autoScroll = true
				m.updateViewportContent()
			}
			return m, nil

		case " ", "s":
			entry := m.selectedEntry()
			if entry == nil {
				return m, nil
			}
			manager := m.manager
			if entry.IsGroup {
				group := entry.Group
				return m, func() tea.Msg {
					procs := manager.ProcessesInGroup(group)
					// If any are running, stop all. Otherwise start all.
					anyUp := false
					for _, p := range procs {
						if p.Status().IsUp() {
							anyUp = true
							break
						}
					}
					if anyUp {
						manager.StopGroup(group)
					} else {
						manager.StartBatch(procs)
					}
					return nil
				}
			}
			proc := entry.Process
			manager2 := m.manager
			return m, func() tea.Msg {
				status := proc.Status()
				if status.IsUp() || status == process.StatusCrashed || status == process.StatusQueued {
					_ = proc.Stop()
					return nil
				}
				manager2.StartBatch([]*process.ManagedProcess{proc})
				return nil
			}

		case "S":
			manager := m.manager
			return m, func() tea.Msg {
				manager.StopAll()
				return nil
			}

		case "A":
			manager := m.manager
			return m, func() tea.Msg {
				manager.StartBatch(manager.Processes)
				return nil
			}

		case "r":
			entry := m.selectedEntry()
			if entry == nil {
				return m, nil
			}
			manager := m.manager
			if entry.IsGroup {
				group := entry.Group
				return m, func() tea.Msg {
					manager.StopGroup(group)
					time.Sleep(500 * time.Millisecond)
					manager.StartBatch(manager.ProcessesInGroup(group))
					return nil
				}
			}
			proc := entry.Process
			return m, func() tea.Msg {
				_ = proc.Stop()
				time.Sleep(500 * time.Millisecond)
				manager.StartBatch([]*process.ManagedProcess{proc})
				return nil
			}

		case "c":
			proc := m.selectedProcess()
			if proc == nil {
				return m, nil
			}
			proc.ClearLogs()
			m.updateViewportContent()
			return m, nil

		case "g":
			m.viewport.GotoTop()
			m.autoScroll = false
			return m, nil

		case "G":
			m.viewport.GotoBottom()
			m.autoScroll = true
			return m, nil

		case "pgup", "ctrl+u":
			m.viewport.HalfViewUp()
			m.autoScroll = false
			return m, nil

		case "pgdown", "ctrl+d":
			m.viewport.HalfViewDown()
			if m.viewport.AtBottom() {
				m.autoScroll = true
			}
			return m, nil
		}

	case process.LogMsg:
		entry := m.selectedEntry()
		if entry == nil {
			return m, nil
		}
		if entry.IsGroup {
			// No live log streaming for group view
		} else if entry.Process.Slug == msg.ProcessSlug && entry.Process.Group == msg.Group {
			m.updateViewportContent()
		}
		return m, nil

	case process.StatusChangeMsg:
		// Refresh group summary if a group is selected
		entry := m.selectedEntry()
		if entry != nil && entry.IsGroup {
			m.updateViewportContent()
		}
		return m, nil
	}

	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		if !m.viewport.AtBottom() {
			m.autoScroll = false
		}
		return m, cmd
	}

	return m, nil
}

func (m *Model) updateViewportContent() {
	if !m.ready {
		return
	}

	entry := m.selectedEntry()
	if entry == nil {
		m.viewport.SetContent("No processes found.")
		return
	}

	if entry.IsGroup {
		// Show group summary
		var content strings.Builder
		procs := m.manager.ProcessesInGroup(entry.Group)
		for _, proc := range procs {
			status := proc.Status()
			var marker string
			switch status {
			case process.StatusRunning:
				marker = "●"
			case process.StatusStarting:
				marker = "◐"
			case process.StatusQueued:
				marker = "◔"
			case process.StatusCrashed:
				marker = "✕"
			default:
				marker = "○"
			}
			content.WriteString(fmt.Sprintf("  %s  %-24s %s\n", marker, proc.Title, status))
		}
		m.viewport.SetContent(content.String())
		return
	}

	proc := entry.Process
	logs := proc.Logs()
	var content strings.Builder
	for _, line := range logs {
		if line.IsSystem {
			content.WriteString(systemLogStyle.Render("▷ " + line.Text))
		} else {
			content.WriteString(line.Text)
		}
		content.WriteString("\n")
	}

	// Preserve scroll position when user has scrolled up
	previousYOffset := m.viewport.YOffset
	m.viewport.SetContent(content.String())
	if m.autoScroll {
		m.viewport.GotoBottom()
	} else {
		m.viewport.SetYOffset(previousYOffset)
	}
}

func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	sidebar := m.renderSidebar()
	logPane := m.renderLogPane()
	main := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, logPane)
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, main, footer)
}

func (m Model) renderSidebar() string {
	var items []string

	title := titleStyle.Render(m.title)
	items = append(items, title)

	selectedSidebarIdx := -1
	if len(m.selectableIndices) > 0 {
		selectedSidebarIdx = m.selectableIndices[m.selectedIndex]
	}

	for i, entry := range m.manager.Sidebar {
		if entry.IsGroup {
			// Add spacing before group headers (except the first)
			if i > 0 {
				items = append(items, "")
			}
			count := m.manager.RunningInGroup(entry.Group)
			chevron := "▾"
			if m.collapsedGroups[entry.Group] {
				chevron = "▸"
			}
			label := fmt.Sprintf("%s %s (%s)", chevron, entry.Group, count)
			if i == selectedSidebarIdx {
				label = selectedItemStyle.Bold(true).Foreground(colorGroup).Render(label)
			} else {
				label = groupHeaderStyle.Render(label)
			}
			items = append(items, label)
			continue
		}

		proc := entry.Process
		status := proc.Status()
		var dot string
		switch status {
		case process.StatusRunning:
			dot = lipgloss.NewStyle().Foreground(colorRunning).Render("●")
		case process.StatusStarting:
			dot = lipgloss.NewStyle().Foreground(colorStarting).Render("◐")
		case process.StatusQueued:
			dot = lipgloss.NewStyle().Foreground(colorQueued).Render("◔")
		case process.StatusCrashed:
			dot = lipgloss.NewStyle().Foreground(colorCrashed).Render("●")
		default:
			dot = lipgloss.NewStyle().Foreground(colorStopped).Render("○")
		}

		prefix := "  "
		if m.multiGroup {
			prefix = "    "
		}
		label := fmt.Sprintf("%s%s %s", prefix, dot, proc.Title)

		if i == selectedSidebarIdx {
			label = selectedItemStyle.Render(label)
		} else {
			label = normalItemStyle.Render(label)
		}

		items = append(items, label)
	}

	content := strings.Join(items, "\n")
	return sidebarStyle.Height(m.height - 2).Render(content)
}

func (m Model) renderLogPane() string {
	entry := m.selectedEntry()
	if entry == nil {
		return ""
	}

	var header string
	if entry.IsGroup {
		count := m.manager.RunningInGroup(entry.Group)
		header = logHeaderStyle.Render(
			fmt.Sprintf("%s — %s", entry.Group, count))
	} else {
		proc := entry.Process
		status := proc.Status()
		var statusText string
		switch status {
		case process.StatusRunning:
			statusText = lipgloss.NewStyle().Foreground(colorRunning).Render("running")
		case process.StatusStarting:
			statusText = lipgloss.NewStyle().Foreground(colorStarting).Render("starting...")
		case process.StatusQueued:
			statusText = lipgloss.NewStyle().Foreground(colorQueued).Render("queued")
		case process.StatusCrashed:
			statusText = lipgloss.NewStyle().Foreground(colorCrashed).Render(
				fmt.Sprintf("crashed (exit %d)", proc.ExitCode()))
		default:
			statusText = lipgloss.NewStyle().Foreground(colorStopped).Render("stopped")
		}

		headerText := proc.Title
		if m.multiGroup {
			headerText = proc.Group + " / " + proc.Title
		}

		header = logHeaderStyle.Render(
			fmt.Sprintf("%s — %s", headerText, statusText))
	}

	if m.alert != "" && time.Now().Before(m.alertExpiry) {
		alertLine := alertStyle.Render("⚠ " + m.alert)
		return lipgloss.JoinVertical(lipgloss.Left, header, alertLine, m.viewport.View())
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, m.viewport.View())
}

func (m Model) renderFooter() string {
	help := "  ↑/↓ select • enter collapse • s start/stop • A start all • S stop all • r restart • c clear • q detach • Q quit"
	summary := m.manager.Summary()

	left := helpStyle.Render(help)
	right := helpStyle.Align(lipgloss.Right).
		Width(m.width - lipgloss.Width(left)).
		Render(summary)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
