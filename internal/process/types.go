package process

import "time"

// Status represents the lifecycle state of a process or task.
type Status int

const (
	StatusStopped  Status = iota
	StatusQueued          // waiting for dependencies
	StatusStarting        // process started, ready check pending
	StatusRunning
	StatusCrashed
	StatusSucceeded // task finished with exit code 0
	StatusFailed    // task finished with non-zero exit code
)

func (s Status) String() string {
	switch s {
	case StatusQueued:
		return "queued"
	case StatusStarting:
		return "starting"
	case StatusRunning:
		return "running"
	case StatusCrashed:
		return "crashed"
	case StatusSucceeded:
		return "succeeded"
	case StatusFailed:
		return "failed"
	default:
		return "stopped"
	}
}

// IsUp returns true if the process is alive (starting or running).
func (s Status) IsUp() bool {
	return s == StatusStarting || s == StatusRunning
}

// LogLine is a single captured output line from a process or task.
type LogLine struct {
	Text      string
	Timestamp time.Time
	IsSystem  bool
}

const maxLogLines = 10000

// processKey returns the unique key for a process/task within a group.
func processKey(group, slug string) string {
	return group + "/" + slug
}
