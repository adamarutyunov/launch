package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/adamarutyunov/launch/internal/process"
	"github.com/charmbracelet/lipgloss"
)

const sidebarWidth = 32

// ANSI 16-color palette — adapts to terminal theme (dark/light).
var (
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
	colorAlert       = lipgloss.Color("3")  // yellow
	// colorGroup is adaptive: light-grey on dark terminals, dark-grey on light ones.
	colorGroup = lipgloss.AdaptiveColor{Dark: "7", Light: "8"}
	// colorSelectedBg is adaptive: dark background on dark themes, light on light.
	colorSelectedBg = lipgloss.AdaptiveColor{Dark: "#27272a", Light: "#d4d4d8"}

	sidebarStyle = lipgloss.NewStyle().
			Width(sidebarWidth).
			BorderRight(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Padding(1, 0)

	selectedItemStyle = lipgloss.NewStyle().
				Background(colorSelectedBg).
				Width(sidebarWidth).
				Padding(0, 1)

	normalItemStyle = lipgloss.NewStyle().
			Width(sidebarWidth).
			Padding(0, 1)

	groupHeaderStyle = lipgloss.NewStyle().
				Foreground(colorGroup).
				Bold(true).
				Width(sidebarWidth).
				Padding(0, 1)

	sectionHeaderStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Width(sidebarWidth).
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

// dot renders a status indicator with an optional selection background.
// Passing bg prevents the ANSI reset inside the dot from revealing the
// terminal's default background between the dot and the title text.
func dot(fg lipgloss.TerminalColor, char string, bg *lipgloss.AdaptiveColor) string {
	s := lipgloss.NewStyle().Foreground(fg)
	if bg != nil {
		s = s.Background(*bg)
	}
	return s.Render(char)
}

func taskDot(status process.Status, spinnerFrame int, bg *lipgloss.AdaptiveColor) string {
	switch status {
	case process.StatusRunning:
		return dot(colorTaskRunning, spinnerGlyph(spinnerFrame), bg)
	case process.StatusSucceeded:
		return dot(colorSucceeded, "●", bg)
	case process.StatusFailed:
		return dot(colorFailed, "●", bg)
	default:
		return dot(colorStopped, "○", bg)
	}
}

func spinnerGlyph(frame int) string {
	frames := []string{"◐", "◓", "◑", "◒"}
	return frames[frame%len(frames)]
}

func processDot(status process.Status, spinnerFrame int, bg *lipgloss.AdaptiveColor) string {
	switch status {
	case process.StatusRunning:
		return dot(colorRunning, "●", bg)
	case process.StatusStarting:
		return dot(colorStarting, spinnerGlyph(spinnerFrame), bg)
	case process.StatusQueued:
		return dot(colorQueued, "◔", bg)
	case process.StatusCrashed:
		return dot(colorCrashed, "●", bg)
	default:
		return dot(colorStopped, "○", bg)
	}
}

func (m Model) renderSidebar() string {
	content := m.sidebarViewport.View()
	if strings.TrimSpace(m.title) != "" {
		title := titleStyle.Render(m.title)
		content = title + "\n" + content
	}
	if m.Embed {
		return lipgloss.NewStyle().
			Width(m.sidebarFrameWidth()).
			Height(max(1, m.height-m.footerLineCount())).
			Padding(1, 0).
			Render(content)
	}
	return sidebarStyle.Height(m.height - 2).Render(content)
}

func (m Model) renderLogPane() string {
	if m.startDialog != nil {
		return m.renderStartDialog()
	}

	entry := m.selectedEntry()
	if entry == nil {
		return ""
	}

	var header string
	if entry.IsGroup {
		count := m.manager.RunningInGroup(entry.Group)
		if count != "" {
			header = logHeaderStyle.Render(fmt.Sprintf("%s — %s", entry.Group, count))
		} else {
			header = logHeaderStyle.Render(entry.Group)
		}
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
		header = logHeaderStyle.Render(fmt.Sprintf("%s — %s", headerText, statusText))
	}

	// For tasks with a subtitle, show it beneath the header.
	var subtitleLine string
	if entry.Item != nil {
		if task, ok := entry.Item.(*process.ManagedTask); ok && task.Subtitle != "" {
			subtitleLine = helpStyle.Render(task.Subtitle)
		}
	}

	var parts []string
	parts = append(parts, header)
	if subtitleLine != "" {
		parts = append(parts, subtitleLine)
	}
	if m.alert != "" && time.Now().Before(m.alertExpiry) {
		parts = append(parts, alertStyle.Render("⚠ "+m.alert))
	}
	parts = append(parts, m.viewport.View())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) renderFooter() string {
	quitKey := "Q"
	if !m.Embed {
		quitKey = "Q/ctrl+c"
	}
	help := buildShortcutHelp(
		shortcutItem{key: "↑/↓", action: "select"},
		shortcutItem{key: "enter", action: "collapse"},
		shortcutItem{key: "s/space", action: "start/stop"},
		shortcutItem{key: "A", action: joinShortcutWords("start", "all")},
		shortcutItem{key: "S", action: joinShortcutWords("stop", "all")},
		shortcutItem{key: "r", action: "restart"},
		shortcutItem{key: "h", action: "hide"},
		shortcutItem{key: "H", action: joinShortcutWords("show", "hidden")},
		shortcutItem{key: "c", action: "clear"},
		shortcutItem{key: "q", action: "detach"},
		shortcutItem{key: quitKey, action: "quit"},
	)
	if m.showHiddenTasks {
		help += " [showing hidden]"
	}
	if m.width <= 0 {
		return helpStyle.Render(help)
	}
	if m.width < 50 && !m.showFooterHelp {
		return helpStyle.Width(max(1, m.width)).Render("  ?: shortcuts")
	}
	return helpStyle.Width(max(1, m.width)).Render(help)
}
