package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/adamarutyunov/launch/internal/process"
	"github.com/adamarutyunov/launch/internal/state"
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
	manager         *process.Manager
	sidebar         []SidebarEntry
	selectedIndex   int
	selectableIndices []int
	sidebarViewport viewport.Model
	viewport        viewport.Model
	width, height   int
	ready           bool
	autoScroll      bool
	spinnerFrame    int
	spinnerActive   bool
	title           string
	multiGroup      bool
	ExitMode        ExitMode
	SavedSession    *state.SessionState
	collapsedGroups map[string]bool
	hiddenTasks     map[string]bool
	showHiddenTasks bool
	alert           string
	alertExpiry     time.Time
	startDialog     *startDialogState
	NoAutoStart     bool
	ForceAutoStart  bool
	Embed           bool
	showFooterHelp  bool
}

func (m Model) footerLineCount() int {
	if m.width <= 0 {
		return 1
	}
	return strings.Count(m.renderFooter(), "\n") + 1
}

func (m Model) sidebarContentWidth() int {
	if m.Embed {
		return max(1, m.width)
	}
	return sidebarWidth
}

func (m Model) sidebarFrameWidth() int {
	if m.Embed {
		return max(1, m.width)
	}
	return sidebarWidth
}

func (m Model) sidebarRowWidth() int {
	if m.Embed {
		return max(1, m.sidebarContentWidth())
	}
	return sidebarWidth
}

func (m Model) sidebarHeight() int {
	titleHeight := 0
	if strings.TrimSpace(m.title) != "" {
		titleRendered := titleStyle.Render(m.title)
		titleHeight = strings.Count(titleRendered, "\n") + 1
	}
	h := m.height - m.footerLineCount() - 2 - titleHeight
	if h < 1 {
		return 1
	}
	return h
}

func NewModel(manager *process.Manager, title string, settings *state.UserSettings) Model {
	if settings.CollapsedGroups == nil {
		settings.CollapsedGroups = make(map[string]bool)
	}
	if settings.HiddenTasks == nil {
		settings.HiddenTasks = make(map[string]bool)
	}

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
	m.updateSidebarContent()
}

func (m *Model) saveSettings() {
	go state.SaveSettings(m.manager.RootDir, &state.UserSettings{
		CollapsedGroups: m.collapsedGroups,
		HiddenTasks:     m.hiddenTasks,
	})
}

type clearAlertMsg struct{}
type spinnerTickMsg struct{}

const spinnerTickInterval = 120 * time.Millisecond

func (m *Model) setAlert(text string) tea.Cmd {
	m.alert = text
	m.alertExpiry = time.Now().Add(5 * time.Second)
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearAlertMsg{}
	})
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerTickInterval, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

func (m Model) hasSpinningItems() bool {
	for _, item := range m.manager.Items {
		status := item.GetStatus()
		if item.Kind() == process.KindProcess && status == process.StatusStarting {
			return true
		}
		if item.Kind() == process.KindTask && status == process.StatusRunning {
			return true
		}
	}
	return false
}

func (m Model) startProcesses(processes []*process.ManagedProcess) {
	if !m.Embed {
		m.manager.StartBatch(processes)
		return
	}
	for _, proc := range processes {
		status := proc.Status()
		if !status.IsUp() && status != process.StatusQueued {
			_ = proc.Start()
		}
	}
}

func (m Model) startGroup(group string) {
	for _, task := range m.manager.Tasks {
		if task.Group == group && !task.GetStatus().IsUp() {
			_ = task.Start()
		}
	}
	m.startProcesses(m.manager.ProcessesInGroup(group))
}

func (m Model) Init() tea.Cmd {
	return func() tea.Msg {
		if m.SavedSession != nil {
			m.manager.ReattachFromState(m.SavedSession)
		} else if m.ForceAutoStart {
			for _, proc := range m.manager.Processes {
				if proc.AutoStart && !proc.Status().IsUp() {
					proc.Start()
				}
			}
		} else if !m.NoAutoStart {
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

	case spinnerTickMsg:
		if !m.spinnerActive {
			return m, nil
		}
		if !m.hasSpinningItems() {
			m.spinnerActive = false
			return m, nil
		}
		m.spinnerFrame = (m.spinnerFrame + 1) % 4
		m.updateSidebarContent()
		m.updateViewportContent()
		return m, spinnerTickCmd()

	case process.AlertMsg:
		cmd := m.setAlert(msg.Text)
		return m, cmd

	case showStartDialogMsg:
		if m.Embed {
			return m, func() tea.Msg {
				_ = msg.process.Start()
				return nil
			}
		}
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
		if m.Embed {
			logWidth = 0
		}
		logHeight := m.height - m.footerLineCount() - 3
		if logHeight < 1 {
			logHeight = 1
		}

		if !m.ready {
			m.viewport = viewport.New(logWidth, logHeight)
			m.viewport.Style = lipgloss.NewStyle()
			m.sidebarViewport = viewport.New(m.sidebarContentWidth(), m.sidebarHeight())
			m.ready = true
		} else {
			m.viewport.Width = logWidth
			m.viewport.Height = logHeight
			m.sidebarViewport.Width = m.sidebarContentWidth()
			m.sidebarViewport.Height = m.sidebarHeight()
		}
		m.updateSidebarContent()
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

		case "Q":
			m.ExitMode = ExitKill
			return m, tea.Quit

		case "ctrl+c":
			if m.Embed {
				return m, nil
			}
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
				m.updateSidebarContent()
				m.updateViewportContent()
			}
			return m, nil

		case "k", "up":
			if m.selectedIndex > 0 {
				m.selectedIndex--
				m.autoScroll = true
				m.updateSidebarContent()
				m.updateViewportContent()
			}
			return m, nil

		case "?":
			if m.width < 50 {
				m.showFooterHelp = !m.showFooterHelp
				if m.ready {
					m.sidebarViewport.Width = m.sidebarContentWidth()
					m.sidebarViewport.Height = m.sidebarHeight()
				}
				m.updateSidebarContent()
				return m, nil
			}

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
						m.startGroup(group)
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
					if status == process.StatusQueued {
						_ = concrete.Stop()
						return nil
					}
					if m.Embed {
						_ = concrete.Start()
						return nil
					}
					unmet := manager.CheckDependencies(concrete)
					if len(unmet) > 0 {
						return showStartDialogMsg{process: concrete}
					}
					m.startProcesses([]*process.ManagedProcess{concrete})
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
			return m, func() tea.Msg {
				m.startProcesses(m.manager.Processes)
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
					m.startGroup(group)
					return nil
				}
			}
			item := entry.Item
			return m, func() tea.Msg {
				_ = item.Stop()
				time.Sleep(500 * time.Millisecond)
				switch concrete := item.(type) {
				case *process.ManagedProcess:
					m.startProcesses([]*process.ManagedProcess{concrete})
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
		m.updateSidebarContent()
		entry := m.selectedEntry()
		if entry != nil && entry.IsGroup {
			m.updateViewportContent()
		}
		_ = m.manager.SaveState()
		if !m.spinnerActive && (msg.Status == process.StatusStarting || msg.Status == process.StatusRunning || m.hasSpinningItems()) {
			m.spinnerActive = true
			return m, spinnerTickCmd()
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

// updateSidebarContent rebuilds the sidebar viewport content and scrolls it
// so the selected entry is visible.
func (m *Model) updateSidebarContent() {
	if !m.ready {
		return
	}

	selectedSidebarIdx := -1
	if len(m.selectableIndices) > 0 {
		selectedSidebarIdx = m.selectableIndices[m.selectedIndex]
	}

	var items []string
	selectedLine := 0

	for i, entry := range m.sidebar {
		if entry.IsSectionHeader {
			items = append(items, "") // blank line above Tasks section
			prefix := "  "
			if m.multiGroup {
				prefix = "    "
			}
			items = append(items, sectionHeaderStyle.Width(m.sidebarRowWidth()).Render(prefix+"Tasks"))
			continue
		}

		if entry.IsGroup {
			if i > 0 {
				items = append(items, "")
			}
			count := m.manager.RunningInGroup(entry.Group)
			chevron := "▾"
			if m.collapsedGroups[entry.Group] {
				chevron = "▸"
			}
			label := fmt.Sprintf("%s %s", chevron, entry.Group)
			if count != "" {
				label = fmt.Sprintf("%s %s (%s)", chevron, entry.Group, count)
			}
			if i == selectedSidebarIdx {
				selectedLine = len(items)
				label = selectedItemStyle.Width(m.sidebarRowWidth()).Bold(true).Foreground(colorGroup).Render(label)
			} else {
				label = groupHeaderStyle.Width(m.sidebarRowWidth()).Render(label)
			}
			items = append(items, label)
			continue
		}

		item := entry.Item
		status := item.GetStatus()

		prefix := ""
		if m.multiGroup {
			prefix = "    "
		}

		var label string
		if entry.Hidden {
			dimLabel := fmt.Sprintf("%s○ %s", prefix, item.GetTitle())
			if i == selectedSidebarIdx {
				selectedLine = len(items)
				label = selectedItemStyle.Width(m.sidebarRowWidth()).Foreground(colorDim).Render(dimLabel)
			} else {
				label = normalItemStyle.Width(m.sidebarRowWidth()).Foreground(colorDim).Render(dimLabel)
			}
		} else if i == selectedSidebarIdx {
			selectedLine = len(items)
			bg := colorSelectedBg
			var d string
			if item.Kind() == process.KindTask {
				d = taskDot(status, m.spinnerFrame, &bg)
			} else {
				d = processDot(status, m.spinnerFrame, &bg)
			}
			selStyle := lipgloss.NewStyle().Background(bg)
			row := selStyle.Render(" "+prefix) + d + selStyle.Render(" "+item.GetTitle())
			totalWidth := m.sidebarRowWidth()
			if vis := lipgloss.Width(row); vis < totalWidth {
				row += selStyle.Render(strings.Repeat(" ", totalWidth-vis))
			}
			label = row
		} else {
			var d string
			if item.Kind() == process.KindTask {
				d = taskDot(status, m.spinnerFrame, nil)
			} else {
				d = processDot(status, m.spinnerFrame, nil)
			}
			label = normalItemStyle.Width(m.sidebarRowWidth()).Render(fmt.Sprintf("%s%s %s", prefix, d, item.GetTitle()))
		}

		items = append(items, label)
	}

	m.sidebarViewport.SetContent(strings.Join(items, "\n"))

	// Scroll so the selected line is visible.
	if selectedLine < m.sidebarViewport.YOffset {
		m.sidebarViewport.SetYOffset(selectedLine)
	}
	if selectedLine >= m.sidebarViewport.YOffset+m.sidebarViewport.Height {
		m.sidebarViewport.SetYOffset(selectedLine - m.sidebarViewport.Height + 1)
	}
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
				marker = lipgloss.NewStyle().Foreground(colorTaskRunning).Render(spinnerGlyph(m.spinnerFrame))
			case status == process.StatusRunning:
				marker = lipgloss.NewStyle().Foreground(colorRunning).Render("●")
			case item.Kind() == process.KindProcess && status == process.StatusStarting:
				marker = lipgloss.NewStyle().Foreground(colorStarting).Render(spinnerGlyph(m.spinnerFrame))
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
	if m.Embed {
		return lipgloss.JoinVertical(lipgloss.Left, sidebar, m.renderFooter())
	}
	logPane := m.renderLogPane()
	main := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, logPane)
	return lipgloss.JoinVertical(lipgloss.Left, main, m.renderFooter())
}

// joinLines joins a slice of strings with newlines.
func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}
