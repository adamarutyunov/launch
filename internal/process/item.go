package process

import tea "github.com/charmbracelet/bubbletea"

// ItemKind distinguishes processes from tasks in the sidebar.
type ItemKind int

const (
	KindProcess ItemKind = iota
	KindTask
)

// SidebarItem is the common interface for processes and tasks displayed in the sidebar.
type SidebarItem interface {
	GetSlug() string
	GetTitle() string
	GetGroup() string
	GetStatus() Status
	GetLogs() []LogLine
	ClearLogs()
	Start(program *tea.Program) error
	Stop() error
	Kind() ItemKind
}
