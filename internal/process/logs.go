package process

import (
	"sync"
	"time"
)

// logBuffer is an append-only, capacity-capped log store embedded by both
// ManagedProcess and ManagedTask to eliminate duplicated log management code.
type logBuffer struct {
	mu   sync.Mutex
	logs []LogLine
}

// write appends a line, evicting the oldest entry when maxLogLines is exceeded.
func (b *logBuffer) write(text string, isSystem bool) LogLine {
	line := LogLine{Text: text, Timestamp: time.Now(), IsSystem: isSystem}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logs = append(b.logs, line)
	if len(b.logs) > maxLogLines {
		b.logs = b.logs[len(b.logs)-maxLogLines:]
	}
	return line
}

// load bulk-appends plain text lines (used when restoring from persisted log files).
func (b *logBuffer) load(lines []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, text := range lines {
		b.logs = append(b.logs, LogLine{Text: text, Timestamp: time.Now()})
	}
	if len(b.logs) > maxLogLines {
		b.logs = b.logs[len(b.logs)-maxLogLines:]
	}
}

// snapshot returns a copy of all buffered log lines.
func (b *logBuffer) snapshot() []LogLine {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]LogLine, len(b.logs))
	copy(result, b.logs)
	return result
}

// clear removes all buffered log lines.
func (b *logBuffer) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logs = b.logs[:0]
}
