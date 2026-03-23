package process

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
	Start() error
	Stop() error
	Kind() ItemKind
}
