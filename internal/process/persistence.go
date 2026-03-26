package process

import (
	"time"

	"github.com/adamarutyunov/launch/internal/state"
)

// SaveState persists the PIDs of all running processes so they can be
// reattached after the TUI restarts.
func (m *Manager) SaveState() error {
	session := &state.SessionState{
		RootDir:   m.RootDir,
		Processes: make(map[string]state.ProcessState),
	}

	for _, proc := range m.Processes {
		if !proc.Status().IsUp() {
			continue
		}
		pid := proc.PID()
		if pid == 0 {
			continue
		}
		session.Processes[state.ProcessKey(proc.Group, proc.Slug)] = state.ProcessState{
			PID:        pid,
			Group:      proc.Group,
			Name:       proc.Slug,
			Command:    proc.Command,
			WorkingDir: proc.WorkingDir,
			LogFile:    proc.LogFile,
			StartedAt:  time.Now(),
		}
	}

	return state.Save(session)
}

// ReattachFromState reconnects to processes that survived a TUI restart.
func (m *Manager) ReattachFromState(session *state.SessionState) int {
	reattached := 0
	for _, proc := range m.Processes {
		saved, exists := session.Processes[state.ProcessKey(proc.Group, proc.Slug)]
		if !exists || !isAlive(saved.PID) {
			continue
		}
		proc.Reattach(saved.PID)
		reattached++
	}
	return reattached
}
