package process

import (
	"fmt"
	"strings"
	"time"
)

// CheckDependencies returns descriptions of unmet dependencies for proc.
// A dependency is met only when the target is StatusRunning (not just StatusStarting).
func (m *Manager) CheckDependencies(proc *ManagedProcess) []string {
	if len(proc.DependsOn) == 0 {
		return nil
	}
	var unmet []string
	for _, dep := range proc.DependsOn {
		depGroup, depSlug := parseDependency(dep, proc.Group)
		target := m.Get(depGroup, depSlug)
		if target == nil {
			unmet = append(unmet, fmt.Sprintf("%s:%s (not found)", depGroup, depSlug))
			continue
		}
		if target.Status() != StatusRunning {
			unmet = append(unmet, fmt.Sprintf("%s:%s (%s)", depGroup, depSlug, target.Title))
		}
	}
	return unmet
}

// parseDependency splits a dep spec into (group, slug).
// "slug" uses the proc's own group; "group:slug" is explicit.
func parseDependency(dep, sameGroup string) (string, string) {
	if idx := strings.Index(dep, ":"); idx != -1 {
		return dep[:idx], dep[idx+1:]
	}
	return sameGroup, dep
}

// StartWithDependencyCheck starts a process if its dependencies are met.
// Returns an alert message on failure, empty string on success.
func (m *Manager) StartWithDependencyCheck(proc *ManagedProcess) string {
	unmet := m.CheckDependencies(proc)
	if len(unmet) > 0 {
		return fmt.Sprintf("Cannot start %s: dependencies not running: %s", proc.Title, strings.Join(unmet, ", "))
	}
	if err := proc.Start(); err != nil {
		return fmt.Sprintf("Failed to start %s: %s", proc.Title, err)
	}
	return ""
}

// StartBatch starts a set of processes respecting dependency order.
// Processes with no unmet dependencies start immediately in parallel.
// As each reaches StatusRunning, any pending processes whose deps are now
// all met get started. Continues until all are started or stuck.
func (m *Manager) StartBatch(processes []*ManagedProcess) {
	if len(processes) == 0 {
		return
	}

	pending := make(map[string]*ManagedProcess)
	for _, proc := range processes {
		status := proc.Status()
		if !status.IsUp() && status != StatusQueued {
			key := processKey(proc.Group, proc.Slug)
			pending[key] = proc
			proc.SetQueued()
		}
	}

	if len(pending) == 0 {
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Minute)
	started := make(map[string]bool)

	for {
		if len(pending) == 0 {
			return
		}

		startedThisRound := false
		for key, proc := range pending {
			if started[key] {
				if proc.Status() == StatusRunning {
					delete(pending, key)
					startedThisRound = true
				} else if proc.Status() == StatusCrashed || proc.Status() == StatusStopped {
					delete(pending, key)
					startedThisRound = true
				}
				continue
			}

			if len(m.CheckDependencies(proc)) == 0 {
				if err := proc.Start(); err != nil {
					proc.logBuf.write(fmt.Sprintf("Failed to start: %s", err), true)
					delete(pending, key)
				} else {
					started[key] = true
					if proc.ReadyCheck == nil {
						delete(pending, key)
					}
				}
				startedThisRound = true
			}
		}

		if len(pending) == 0 {
			return
		}

		allBlocked := true
		for key, proc := range pending {
			if started[key] {
				allBlocked = false
				continue
			}
			for _, dep := range proc.DependsOn {
				depGroup, depSlug := parseDependency(dep, proc.Group)
				if started[processKey(depGroup, depSlug)] {
					allBlocked = false
					break
				}
			}
		}

		if allBlocked && !startedThisRound {
			for key, proc := range pending {
				if !started[key] {
					unmet := m.CheckDependencies(proc)
					proc.logBuf.write(fmt.Sprintf("Cannot start: dependencies not running: %s", strings.Join(unmet, ", ")), true)
					proc.mu.Lock()
					proc.status = StatusStopped
					proc.mu.Unlock()
					m.Notifier.Send(StatusChangeMsg{ProcessSlug: proc.Slug, Group: proc.Group, Status: StatusStopped})
				}
			}
			return
		}

		select {
		case <-ticker.C:
			continue
		case <-deadline:
			for key, proc := range pending {
				if !started[key] {
					proc.logBuf.write("Timed out waiting for dependencies", true)
					proc.mu.Lock()
					proc.status = StatusStopped
					proc.mu.Unlock()
					m.Notifier.Send(StatusChangeMsg{ProcessSlug: proc.Slug, Group: proc.Group, Status: StatusStopped})
				}
			}
			return
		}
	}
}

// DependencyTree returns all transitive dependencies of proc plus proc itself,
// in dependency-first order. Handles cycles via a visited set.
func (m *Manager) DependencyTree(proc *ManagedProcess) []*ManagedProcess {
	var result []*ManagedProcess
	visited := make(map[string]bool)
	m.collectDependencyTree(proc, visited, &result)
	return result
}

func (m *Manager) collectDependencyTree(proc *ManagedProcess, visited map[string]bool, result *[]*ManagedProcess) {
	key := processKey(proc.Group, proc.Slug)
	if visited[key] {
		return
	}
	visited[key] = true
	for _, dep := range proc.DependsOn {
		depGroup, depSlug := parseDependency(dep, proc.Group)
		if depProc := m.Get(depGroup, depSlug); depProc != nil {
			m.collectDependencyTree(depProc, visited, result)
		}
	}
	*result = append(*result, proc)
}
