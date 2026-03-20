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

	// ANSI 16-color palette — adapts to terminal theme (dark/light).
	colorRunning     = lipgloss.Color("10") // bright green
	colorTaskRunning = lipgloss.Color("14") // bright cyan (task in-progress)
	colorStarting    = lipgloss.Color("3")  // yellow
	colorQueued      = lipgloss.Color("8")  // dark grey
	colorStopped     = lipgloss.Color("8")  // dark grey
	colorCrashed     = lipgloss.Color("9")  // bright red
	colorSucceeded   = lipgloss.Color("10") // bright green
	colorFailed      = lipgloss.Color("9")  // bright red
	colorDim         = lipgloss.Color("8")  // dark grey
	colorBorder      = lipgloss.Color("8")  // dark grey
	// colorGroup is adaptive: light-grey on dark terminals, dark-grey on light ones.
	colorGroup = lipgloss.AdaptiveColor{Dark: "7", Light: "8"}
	colorAlert       = lipgloss.Color("3")  // yellow

	// Adaptive selection background: dark on dark themes, light on light themes.
	// Adaptive selection background.
	colorSelectedBg = lipgloss.AdaptiveColor{Dark: "#27272a", Light: "#d4d4d8"}

	sidebarStyle = lipgloss.NewStyle().
			Width(sidebarWidth).
			BorderRight(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Padding(1, 0)

	selectedItemStyle = lipgloss.NewStyle().
				Background(colorSelectedBg).
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

	sectionHeaderStyle = lipgloss.NewStyle().
				Foreground(colorDim).
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
	hiddenTasks       map[string]bool
	showHiddenTasks   bool
	alert             string
	alertExpiry       time.Time
}

func NewModel(manager *process.Manager, title string, settings *state.UserSettings) Model {
	collapsedGroups := settings.CollapsedGroups
	hiddenTasks := settings.HiddenTasks
	manager.BuildSidebar(collapsedGroups, hiddenTasks, false)
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
		hiddenTasks:       hiddenTasks,
	}
}

func (m Model) selectedEntry() *process.SidebarEntry {
	if len(m.selectableIndices) == 0 {
		return nil
	}
	sidebarIdx := m.selectableIndices[m.selectedIndex]
	return &m.manager.Sidebar[sidebarIdx]
}

func (m *Model) rebuildSidebar() {
	// Save identity of the currently selected entry so we can restore it.
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

	m.manager.BuildSidebar(m.collapsedGroups, m.hiddenTasks, m.showHiddenTasks)
	m.selectableIndices = m.manager.SelectableIndices()

	// Try to find the exact same entry.
	found := false
	for i, idx := range m.selectableIndices {
		entry := m.manager.Sidebar[idx]
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

	// If the item is gone (hidden), keep the same numeric position so the
	// cursor stays near where it was rather than jumping to the top.
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
					items := manager.ItemsInGroup(group)
					anyUp := false
					for _, item := range items {
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
					manager.StartBatch([]*process.ManagedProcess{concrete})
				case *process.ManagedTask:
					_ = concrete.Start(manager.Program)
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
				for _, task := range manager.Tasks {
					if !task.GetStatus().IsUp() {
						_ = task.Start(manager.Program)
					}
				}
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
					_ = concrete.Start(manager.Program)
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
		var content strings.Builder
		items := m.manager.ItemsInGroup(entry.Group)
		for _, item := range items {
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
			content.WriteString(fmt.Sprintf("  %s  %-24s %s\n", marker, item.GetTitle(), status))
		}
		m.viewport.SetContent(content.String())
		return
	}

	item := entry.Item
	logs := item.GetLogs()
	var content strings.Builder
	for _, line := range logs {
		if line.IsSystem {
			content.WriteString(systemLogStyle.Render("▷ " + line.Text))
		} else {
			content.WriteString(line.Text)
		}
		content.WriteString("\n")
	}

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

// dot renders a status indicator with an optional background color.
// bg should be nil for normal (unselected) items and colorSelectedBg for selected ones.
// Setting bg on the dot prevents the ANSI reset inside the dot from revealing the
// terminal's default background (white on light themes) mid-row.
func dot(fg lipgloss.TerminalColor, char string, bg *lipgloss.AdaptiveColor) string {
	s := lipgloss.NewStyle().Foreground(fg)
	if bg != nil {
		s = s.Background(*bg)
	}
	return s.Render(char)
}

func taskDot(status process.Status, bg *lipgloss.AdaptiveColor) string {
	switch status {
	case process.StatusRunning:
		return dot(colorTaskRunning, "●", bg)
	case process.StatusSucceeded:
		return dot(colorSucceeded, "●", bg)
	case process.StatusFailed:
		return dot(colorFailed, "●", bg)
	default:
		return dot(colorStopped, "○", bg)
	}
}

func processDot(status process.Status, bg *lipgloss.AdaptiveColor) string {
	switch status {
	case process.StatusRunning:
		return dot(colorRunning, "●", bg)
	case process.StatusStarting:
		return dot(colorStarting, "◐", bg)
	case process.StatusQueued:
		return dot(colorQueued, "◔", bg)
	case process.StatusCrashed:
		return dot(colorCrashed, "●", bg)
	default:
		return dot(colorStopped, "○", bg)
	}
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
		if entry.IsSectionHeader {
			items = append(items, "") // blank line above Tasks
			prefix := "  "
			if m.multiGroup {
				prefix = "    "
			}
			label := sectionHeaderStyle.Render(prefix + "Tasks")
			items = append(items, label)
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
			label := fmt.Sprintf("%s %s (%s)", chevron, entry.Group, count)
			if i == selectedSidebarIdx {
				label = selectedItemStyle.Bold(true).Foreground(colorGroup).Render(label)
			} else {
				label = groupHeaderStyle.Render(label)
			}
			items = append(items, label)
			continue
		}

		item := entry.Item
		status := item.GetStatus()

		prefix := "  "
		if m.multiGroup {
			prefix = "    "
		}

		var label string
		if entry.Hidden {
			// Hidden tasks: always dim, plain dot so no inner ANSI resets.
			dimLabel := fmt.Sprintf("%s○ %s", prefix, item.GetTitle())
			if i == selectedSidebarIdx {
				label = selectedItemStyle.Foreground(colorDim).Render(dimLabel)
			} else {
				label = normalItemStyle.Foreground(colorDim).Render(dimLabel)
			}
		} else if i == selectedSidebarIdx {
			// Selected item: every piece carries the selection background so that
			// the ANSI reset inside the dot character never reveals the bare
			// terminal background (white on light themes) between dot and title.
			bg := colorSelectedBg
			var d string
			if item.Kind() == process.KindTask {
				d = taskDot(status, &bg)
			} else {
				d = processDot(status, &bg)
			}
			// " "+prefix mirrors the Padding(0,1) left-pad that normalItemStyle adds.
			selStyle := lipgloss.NewStyle().Background(bg)
			row := selStyle.Render(" "+prefix) + d + selStyle.Render(" "+item.GetTitle())
			// Pad the row to the full sidebar item width.
			totalWidth := sidebarWidth - 2
			if vis := lipgloss.Width(row); vis < totalWidth {
				row += selStyle.Render(strings.Repeat(" ", totalWidth-vis))
			}
			label = row
		} else {
			var d string
			if item.Kind() == process.KindTask {
				d = taskDot(status, nil)
			} else {
				d = processDot(status, nil)
			}
			label = normalItemStyle.Render(fmt.Sprintf("%s%s %s", prefix, d, item.GetTitle()))
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
		item := entry.Item
		status := item.GetStatus()
		var statusText string

		if item.Kind() == process.KindTask {
			switch status {
			case process.StatusRunning:
				statusText = lipgloss.NewStyle().Foreground(colorTaskRunning).Render("running")
			case process.StatusSucceeded:
				statusText = lipgloss.NewStyle().Foreground(colorSucceeded).Render("succeeded")
			case process.StatusFailed:
				task := item.(*process.ManagedTask)
				statusText = lipgloss.NewStyle().Foreground(colorFailed).Render(
					fmt.Sprintf("failed (exit %d)", task.ExitCode()))
			default:
				statusText = lipgloss.NewStyle().Foreground(colorStopped).Render("stopped")
			}
		} else {
			proc := item.(*process.ManagedProcess)
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
		}

		headerText := item.GetTitle()
		if m.multiGroup {
			headerText = item.GetGroup() + " / " + item.GetTitle()
		}

		header = logHeaderStyle.Render(
			fmt.Sprintf("%s — %s", headerText, statusText))
	}

	// For tasks with a description, show it as a subtitle under the header.
	var descLine string
	if entry.Item != nil {
		if task, ok := entry.Item.(*process.ManagedTask); ok && task.Description != "" {
			descLine = helpStyle.Render(task.Description)
		}
	}

	var parts []string
	parts = append(parts, header)
	if descLine != "" {
		parts = append(parts, descLine)
	}
	if m.alert != "" && time.Now().Before(m.alertExpiry) {
		parts = append(parts, alertStyle.Render("⚠ "+m.alert))
	}
	parts = append(parts, m.viewport.View())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) renderFooter() string {
	help := "  ↑/↓ select • enter collapse • s/space start/stop • A start all • S stop all • r restart • h hide • H show hidden • c clear • q detach • Q quit"
	if m.showHiddenTasks {
		help += " [showing hidden]"
	}
	summary := m.manager.Summary()

	left := helpStyle.Render(help)
	right := helpStyle.Align(lipgloss.Right).
		Width(m.width - lipgloss.Width(left)).
		Render(summary)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
