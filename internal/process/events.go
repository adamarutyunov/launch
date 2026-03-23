package process

// Notifier receives status, log, and alert events from managed processes and tasks.
// The UI registers a concrete implementation (wrapping *tea.Program) via Manager.SetNotifier.
type Notifier interface {
	Send(msg any)
}

// LogMsg is sent when a process or task produces a log line.
type LogMsg struct {
	ProcessSlug string
	Group       string
	Line        LogLine
}

// StatusChangeMsg is sent when a process or task transitions to a new status.
type StatusChangeMsg struct {
	ProcessSlug string
	Group       string
	Status      Status
	ExitCode    int
}

// AlertMsg is sent to display a transient alert banner in the UI.
type AlertMsg struct {
	Text string
}
