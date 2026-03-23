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

type ExitMode int

const (
	ExitDetach ExitMode = iota
	ExitKill
)

type Model struct {
	manager           *process.Manager
	sidebar           []SidebarEntry
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
	hiddenTasks       map[string]bool
	showHiddenTasks   bool
	alert             string
	alertExpiry       time.Time
	startDialog       *startDialogState
}

func NewModel(manager *process.Manager, title string, settings *state.UserSettings) Model {
	sidebar := buildSidebar(manager, settings.CollapsedGroups, settings.HiddenTasks, false)
	selectable := selectableIndices(sidebar)

	return Model{
		manager:           manager,
		sidebar:           sidebar,
		selectedIndex:     0,
		selectableIndices: selectable,
		autoScroll:        true,
		title:             title,
		multiGroup:        len(manager.GroupNames()) > 1,
		ExitMode:          ExitDetach,
		collapsedGroups:   settings.CollapsedGroups,
		hiddenTasks:       settings.HiddenTasks,
	}
}

func (m Model) selectedEntry() *SidebarEntry {
	if len(m.selectableIndices) == 0 {
		return nil
	}
	return &m.sidebar[m.selectableIndices[m.selectedIndex]]
}

func (m *Model) rebuildSidebar() {
	previousIndex := m.selectedIndex
	selectedIsGroup := false
	selectedGroupName := ""
	selectedItemKey := ""
	if entry := m.selectedEntry(); entry != nil {
		selectedIsGroup = entry.IsGroup
		selectedGroupName = entry.Group
		if !entry.IsGroup && entry.Item != nil {
			selectedItemKey = entry.Item.GetGroup() + "/" + entry.Item.GetSlug()
		}
	}

	m.sidebar = buildSidebar(m.manager, m.collapsedGroups, m.hiddenTasks, m.showHiddenTasks)
	m.selectableIndices = selectableIndices(m.sidebar)

	found := false
	for i, idx := range m.selectableIndices {
		entry := m.sidebar[idx]
		if selectedIsGroup {
			if entry.IsGroup && entry.Group == selectedGroupName {
				m.selectedIndex = i
				found = true
				break
			}
		} else if selectedItemKey != "" {
			if !entry.IsGroup && entry.Item != nil {
				if entry.Item.GetGroup()+"/"+entry.Item.GetSlug() == selectedItemKey {
					m.selectedIndex = i
					found = true
					break
				}
			}
		}
	}

	if !found {
		m.selectedIndex = previousIndex
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
		HiddenTasks:     m.hiddenTasks,
	})
}

type clearAlertMsg struct{}

func (m *Model) setAlert(text string) tea.Cmd {
	m.alert = text
	m.alertExpiry = time.Now().Add(5 * time.Second)
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearAlertMsg{}
	})
}

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

	case showStartDialogMsg:
		tree := m.manager.DependencyTree(msg.process)
		m.startDialog = &startDialogState{
			process:        msg.process,
			dependencyTree: tree,
		}
		m.updateViewportContent()
		return m, nil

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
		// Intercept all key presses while the start dialog is visible.
		if model, cmd, handled := m.handleDialogKey(msg.String()); handled {
			return model, cmd
		}

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
			m.collapsedGroups[entry.Group] = !m.collapsedGroups[entry.Group]
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

		case "h":
			entry := m.selectedEntry()
			if entry == nil || entry.IsGroup || entry.Item == nil {
				return m, nil
			}
			if entry.Item.Kind() != process.KindTask {
				return m, nil
			}
			taskKey := entry.Item.GetGroup() + "/" + entry.Item.GetSlug()
			m.hiddenTasks[taskKey] = !m.hiddenTasks[taskKey]
			m.rebuildSidebar()
			m.updateViewportContent()
			m.saveSettings()
			return m, nil

		case "H":
			m.showHiddenTasks = !m.showHiddenTasks
			m.rebuildSidebar()
			m.updateViewportContent()
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
					anyUp := false
					for _, item := range manager.ItemsInGroup(group) {
						if item.GetStatus().IsUp() {
							anyUp = true
							break
						}
					}
					if anyUp {
						manager.StopGroup(group)
					} else {
						manager.StartGroup(group)
					}
					return nil
				}
			}
			item := entry.Item
			return m, func() tea.Msg {
				status := item.GetStatus()
				if status.IsUp() {
					_ = item.Stop()
					return nil
				}
				switch concrete := item.(type) {
				case *process.ManagedProcess:
					if status == process.StatusCrashed || status == process.StatusQueued {
						_ = concrete.Stop()
						return nil
					}
					unmet := manager.CheckDependencies(concrete)
					if len(unmet) > 0 {
						return showStartDialogMsg{process: concrete}
					}
					manager.StartBatch([]*process.ManagedProcess{concrete})
				case *process.ManagedTask:
					_ = concrete.Start()
				}
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
					manager.StartGroup(group)
					return nil
				}
			}
			item := entry.Item
			return m, func() tea.Msg {
				_ = item.Stop()
				time.Sleep(500 * time.Millisecond)
				switch concrete := item.(type) {
				case *process.ManagedProcess:
					manager.StartBatch([]*process.ManagedProcess{concrete})
				case *process.ManagedTask:
					_ = concrete.Start()
				}
				return nil
			}

		case "c":
			entry := m.selectedEntry()
			if entry == nil || entry.IsGroup {
				return m, nil
			}
			entry.Item.ClearLogs()
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
			m.viewport.HalfPageUp()
			m.autoScroll = false
			return m, nil

		case "pgdown", "ctrl+d":
			m.viewport.HalfPageDown()
			if m.viewport.AtBottom() {
				m.autoScroll = true
			}
			return m, nil
		}

	case process.LogMsg:
		entry := m.selectedEntry()
		if entry == nil || entry.IsGroup {
			return m, nil
		}
		if entry.Item.GetSlug() == msg.ProcessSlug && entry.Item.GetGroup() == msg.Group {
			m.updateViewportContent()
		}
		return m, nil

	case process.StatusChangeMsg:
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
		var lines []string
		for _, item := range m.manager.ItemsInGroup(entry.Group) {
			status := item.GetStatus()
			var marker string
			switch {
			case status == process.StatusRunning && item.Kind() == process.KindTask:
				marker = lipgloss.NewStyle().Foreground(colorTaskRunning).Render("●")
			case status == process.StatusRunning:
				marker = lipgloss.NewStyle().Foreground(colorRunning).Render("●")
			case item.Kind() == process.KindProcess && status == process.StatusStarting:
				marker = lipgloss.NewStyle().Foreground(colorStarting).Render("◐")
			case item.Kind() == process.KindProcess && status == process.StatusQueued:
				marker = lipgloss.NewStyle().Foreground(colorQueued).Render("◔")
			case item.Kind() == process.KindProcess && status == process.StatusCrashed:
				marker = lipgloss.NewStyle().Foreground(colorCrashed).Render("✕")
			case status == process.StatusSucceeded:
				marker = lipgloss.NewStyle().Foreground(colorSucceeded).Render("●")
			case status == process.StatusFailed:
				marker = lipgloss.NewStyle().Foreground(colorFailed).Render("✕")
			default:
				marker = lipgloss.NewStyle().Foreground(colorStopped).Render("○")
			}
			lines = append(lines, fmt.Sprintf("  %s  %-24s %s", marker, item.GetTitle(), status))
		}
		m.viewport.SetContent(joinLines(lines))
		return
	}

	item := entry.Item
	logs := item.GetLogs()
	var lines []string
	for _, line := range logs {
		if line.IsSystem {
			lines = append(lines, systemLogStyle.Render("▷ "+line.Text))
		} else {
			lines = append(lines, line.Text)
		}
	}

	previousYOffset := m.viewport.YOffset
	m.viewport.SetContent(joinLines(lines))
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
	return lipgloss.JoinVertical(lipgloss.Left, main, m.renderFooter())
}

// padRight pads s with spaces to width w (truncates if longer).
func padRight(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

// joinLines joins a slice of strings with newlines.
func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}
