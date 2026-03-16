package ui

import (
	"fmt"
	"strings"

	"github.com/adam/launch/internal/process"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	sidebarWidth = 32

	colorRunning  = lipgloss.Color("#22c55e")
	colorStopped  = lipgloss.Color("#71717a")
	colorCrashed  = lipgloss.Color("#ef4444")
	colorDim      = lipgloss.Color("#52525b")
	colorBorder   = lipgloss.Color("#3f3f46")
	colorGroup    = lipgloss.Color("#a1a1aa")

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
				Padding(0, 1).
				MarginTop(1)

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
)

type Model struct {
	manager           *process.Manager
	selectedIndex     int // index into selectableIndices
	selectableIndices []int
	viewport          viewport.Model
	width, height     int
	ready             bool
	autoScroll        bool
	title             string
	multiGroup        bool
}

func NewModel(manager *process.Manager, title string) Model {
	manager.BuildSidebar()
	selectable := manager.SelectableIndices()

	return Model{
		manager:           manager,
		selectedIndex:     0,
		selectableIndices: selectable,
		autoScroll:        true,
		title:             title,
		multiGroup:        len(manager.Groups) > 1,
	}
}

func (m Model) selectedProcess() *process.ManagedProcess {
	if len(m.selectableIndices) == 0 {
		return nil
	}
	sidebarIdx := m.selectableIndices[m.selectedIndex]
	return m.manager.Sidebar[sidebarIdx].Process
}

func (m Model) Init() tea.Cmd {
	return func() tea.Msg {
		// StartAll uses manager.Program which is set before Run()
		m.manager.StartAll()
		return nil
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		case "q", "ctrl+c":
			m.manager.StopAll()
			return m, tea.Quit

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

		case "s":
			proc := m.selectedProcess()
			if proc == nil {
				return m, nil
			}
			program := m.manager.Program
			return m, func() tea.Msg {
				if proc.Status() == process.StatusRunning {
					_ = proc.Stop()
				} else {
					_ = proc.Start(program)
				}
				return nil
			}

		case "r":
			proc := m.selectedProcess()
			if proc == nil {
				return m, nil
			}
			program := m.manager.Program
			return m, func() tea.Msg {
				_ = proc.Restart(program)
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
		proc := m.selectedProcess()
		if proc != nil && proc.Name == msg.ProcessName && proc.Group == msg.Group {
			m.updateViewportContent()
		}
		return m, nil

	case process.StatusChangeMsg:
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

	proc := m.selectedProcess()
	if proc == nil {
		m.viewport.SetContent("No processes found.")
		return
	}

	logs := proc.Logs()
	var content strings.Builder
	for _, line := range logs {
		content.WriteString(line.Text)
		content.WriteString("\n")
	}

	m.viewport.SetContent(content.String())
	if m.autoScroll {
		m.viewport.GotoBottom()
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
			count := m.manager.RunningInGroup(entry.Group)
			header := groupHeaderStyle.Render(fmt.Sprintf("▸ %s (%s)", entry.Group, count))
			items = append(items, header)
			continue
		}

		proc := entry.Process
		status := proc.Status()
		var dot string
		switch status {
		case process.StatusRunning:
			dot = lipgloss.NewStyle().Foreground(colorRunning).Render("●")
		case process.StatusCrashed:
			dot = lipgloss.NewStyle().Foreground(colorCrashed).Render("●")
		default:
			dot = lipgloss.NewStyle().Foreground(colorStopped).Render("○")
		}

		prefix := "  "
		if m.multiGroup {
			prefix = "    "
		}
		label := fmt.Sprintf("%s%s %s", prefix, dot, proc.Name)

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
	proc := m.selectedProcess()
	if proc == nil {
		return ""
	}

	status := proc.Status()
	var statusText string
	switch status {
	case process.StatusRunning:
		statusText = lipgloss.NewStyle().Foreground(colorRunning).Render("running")
	case process.StatusCrashed:
		statusText = lipgloss.NewStyle().Foreground(colorCrashed).Render(
			fmt.Sprintf("crashed (exit %d)", proc.ExitCode()))
	default:
		statusText = lipgloss.NewStyle().Foreground(colorStopped).Render("stopped")
	}

	headerText := proc.Name
	if m.multiGroup {
		headerText = proc.Group + " / " + proc.Name
	}

	header := logHeaderStyle.Render(
		fmt.Sprintf("%s — %s", headerText, statusText))

	return lipgloss.JoinVertical(lipgloss.Left, header, m.viewport.View())
}

func (m Model) renderFooter() string {
	help := "  ↑/↓ select • s start/stop • r restart • c clear • g/G top/bottom • q quit"
	summary := m.manager.Summary()

	left := helpStyle.Render(help)
	right := helpStyle.Align(lipgloss.Right).
		Width(m.width - lipgloss.Width(left)).
		Render(summary)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
