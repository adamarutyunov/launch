package process

import (
	"fmt"
	"sync"

	"github.com/adam/launch/internal/state"
)

// Manager owns all processes and tasks for a launch session and routes events
// to the registered Notifier.
type Manager struct {
	Processes []*ManagedProcess
	Tasks     []*ManagedTask
	Items     []SidebarItem
	Notifier  Notifier
	RootDir   string
	byKey     map[string]*ManagedProcess
}

func NewManager(rootDir string) *Manager {
	return &Manager{
		RootDir: rootDir,
		byKey:   make(map[string]*ManagedProcess),
	}
}

// SetNotifier registers the event sink and propagates it to all existing
// processes and tasks. Call this after the tea.Program is created.
func (m *Manager) SetNotifier(n Notifier) {
	m.Notifier = n
	for _, proc := range m.Processes {
		proc.notifier = n
	}
	for _, task := range m.Tasks {
		task.notifier = n
	}
}

func (m *Manager) Add(proc *ManagedProcess) {
	m.Processes = append(m.Processes, proc)
	m.Items = append(m.Items, proc)
	m.byKey[processKey(proc.Group, proc.Slug)] = proc
}

func (m *Manager) AddTask(task *ManagedTask) {
	m.Tasks = append(m.Tasks, task)
	m.Items = append(m.Items, task)
}

func (m *Manager) Get(group, slug string) *ManagedProcess {
	return m.byKey[processKey(group, slug)]
}

// GroupNames returns the distinct group names in the order they were added.
func (m *Manager) GroupNames() []string {
	seen := make(map[string]bool)
	var groups []string
	for _, item := range m.Items {
		g := item.GetGroup()
		if !seen[g] {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	return groups
}

// ProcessesInGroup returns all processes belonging to a group.
func (m *Manager) ProcessesInGroup(group string) []*ManagedProcess {
	var result []*ManagedProcess
	for _, proc := range m.Processes {
		if proc.Group == group {
			result = append(result, proc)
		}
	}
	return result
}

// TasksInGroup returns all tasks belonging to a group.
func (m *Manager) TasksInGroup(group string) []*ManagedTask {
	var result []*ManagedTask
	for _, task := range m.Tasks {
		if task.Group == group {
			result = append(result, task)
		}
	}
	return result
}

// ItemsInGroup returns all sidebar items (processes and tasks) belonging to a group.
func (m *Manager) ItemsInGroup(group string) []SidebarItem {
	var result []SidebarItem
	for _, item := range m.Items {
		if item.GetGroup() == group {
			result = append(result, item)
		}
	}
	return result
}

// StopGroup stops all running items in a group.
func (m *Manager) StopGroup(group string) {
	var wg sync.WaitGroup
	for _, item := range m.Items {
		if item.GetGroup() == group && item.GetStatus().IsUp() {
			wg.Add(1)
			go func(i SidebarItem) {
				defer wg.Done()
				_ = i.Stop()
			}(item)
		}
	}
	wg.Wait()
}

// StartGroup starts all items in a group — processes via dependency-ordered
// batch, tasks directly.
func (m *Manager) StartGroup(group string) {
	for _, task := range m.Tasks {
		if task.Group == group && !task.GetStatus().IsUp() {
			_ = task.Start()
		}
	}
	m.StartBatch(m.ProcessesInGroup(group))
}

// StartAutoStart starts all auto_start processes using dependency-ordered batch launch.
func (m *Manager) StartAutoStart() {
	var toStart []*ManagedProcess
	for _, proc := range m.Processes {
		if proc.AutoStart && !proc.Status().IsUp() {
			toStart = append(toStart, proc)
		}
	}
	m.StartBatch(toStart)
}

// StopAll stops all running items and removes saved state.
func (m *Manager) StopAll() {
	var wg sync.WaitGroup
	for _, item := range m.Items {
		wg.Add(1)
		go func(i SidebarItem) {
			defer wg.Done()
			_ = i.Stop()
		}(item)
	}
	wg.Wait()
	_ = state.Remove(m.RootDir)
}

// Summary returns a "N/M running" string for the status bar.
func (m *Manager) Summary() string {
	running := 0
	for _, item := range m.Items {
		if item.GetStatus().IsUp() {
			running++
		}
	}
	return fmt.Sprintf("%d/%d running", running, len(m.Items))
}

// RunningInGroup returns "N/M" running processes in a group, or "" if the
// group has no processes (tasks-only group).
func (m *Manager) RunningInGroup(group string) string {
	running := 0
	total := 0
	for _, proc := range m.Processes {
		if proc.Group == group {
			total++
			if proc.GetStatus().IsUp() {
				running++
			}
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", running, total)
}
