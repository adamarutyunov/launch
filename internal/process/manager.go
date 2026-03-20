package process

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/adam/launch/internal/config"
	"github.com/adam/launch/internal/state"
	tea "github.com/charmbracelet/bubbletea"
)

type Status int

const (
	StatusStopped Status = iota
	StatusQueued
	StatusStarting
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

const maxLogLines = 10000

type LogLine struct {
	Text      string
	Timestamp time.Time
	IsSystem  bool
}

type ManagedProcess struct {
	Slug       string
	Title      string
	Group      string
	Command    string
	WorkingDir string
	Env        map[string]string
	AutoStart  bool
	DependsOn  []string
	ReadyCheck *config.ReadyCheck
	LogFile    string

	mu            sync.Mutex
	cmd           *exec.Cmd
	pid           int
	reattached    bool
	status        Status
	logs          []LogLine
	exitCode      int
	logFileHandle *os.File
	tailDone      chan struct{}
	tailDoneOnce  sync.Once
}

type LogMsg struct {
	ProcessSlug string
	Group       string
	Line        LogLine
}

type StatusChangeMsg struct {
	ProcessSlug string
	Group       string
	Status      Status
	ExitCode    int
}

type AlertMsg struct {
	Text string
}

// GetSlug implements SidebarItem.
func (p *ManagedProcess) GetSlug() string { return p.Slug }

// GetTitle implements SidebarItem.
func (p *ManagedProcess) GetTitle() string { return p.Title }

// GetGroup implements SidebarItem.
func (p *ManagedProcess) GetGroup() string { return p.Group }

// GetStatus implements SidebarItem.
func (p *ManagedProcess) GetStatus() Status { return p.Status() }

// GetLogs implements SidebarItem.
func (p *ManagedProcess) GetLogs() []LogLine { return p.Logs() }

// Kind implements SidebarItem.
func (p *ManagedProcess) Kind() ItemKind { return KindProcess }

func NewManagedProcess(slug, title, group, command, workingDir string, env map[string]string, autoStart bool, dependsOn []string, readyCheck *config.ReadyCheck, logFile string) *ManagedProcess {
	return &ManagedProcess{
		Slug:       slug,
		Title:      title,
		Group:      group,
		Command:    command,
		WorkingDir: workingDir,
		Env:        env,
		AutoStart:  autoStart,
		DependsOn:  dependsOn,
		ReadyCheck: readyCheck,
		LogFile:    logFile,
		status:     StatusStopped,
		logs:       make([]LogLine, 0, 256),
	}
}

func (p *ManagedProcess) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *ManagedProcess) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

func (p *ManagedProcess) ExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode
}

func (p *ManagedProcess) Logs() []LogLine {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]LogLine, len(p.logs))
	copy(result, p.logs)
	return result
}

func (p *ManagedProcess) appendLog(text string) LogLine {
	line := LogLine{Text: text, Timestamp: time.Now()}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logs = append(p.logs, line)
	if len(p.logs) > maxLogLines {
		p.logs = p.logs[len(p.logs)-maxLogLines:]
	}
	return line
}

func (p *ManagedProcess) appendSystemLog(text string) LogLine {
	line := LogLine{Text: text, Timestamp: time.Now(), IsSystem: true}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logs = append(p.logs, line)
	if len(p.logs) > maxLogLines {
		p.logs = p.logs[len(p.logs)-maxLogLines:]
	}
	return line
}

func (p *ManagedProcess) ClearLogs() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logs = p.logs[:0]
}

func (p *ManagedProcess) SetQueued(program *tea.Program) {
	p.mu.Lock()
	p.status = StatusQueued
	p.mu.Unlock()
	program.Send(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: StatusQueued})
}

func (p *ManagedProcess) closeTailDone() {
	p.tailDoneOnce.Do(func() {
		if p.tailDone != nil {
			close(p.tailDone)
		}
	})
}

func (p *ManagedProcess) resetTailDone() {
	p.tailDone = make(chan struct{})
	p.tailDoneOnce = sync.Once{}
}

func (p *ManagedProcess) Reattach(pid int, program *tea.Program) {
	p.mu.Lock()
	p.pid = pid
	p.status = StatusRunning // assume ready for reattached processes
	p.reattached = true
	p.mu.Unlock()

	p.loadLogTail()

	p.resetTailDone()
	go p.tailLogFile(program)
	go p.monitorPID(program)

	program.Send(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: StatusRunning})
}

func (p *ManagedProcess) Start(program *tea.Program) error {
	p.mu.Lock()
	if p.status.IsUp() {
		p.mu.Unlock()
		return fmt.Errorf("process %s is already running", p.Title)
	}
	p.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(p.LogFile), 0755); err != nil {
		return fmt.Errorf("creating log dir: %w", err)
	}

	logFile, err := os.Create(p.LogFile)
	if err != nil {
		return fmt.Errorf("creating log file: %w", err)
	}

	cmd := exec.Command("sh", "-c", p.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if p.WorkingDir != "" {
		cmd.Dir = p.WorkingDir
	}
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "FORCE_COLOR=1", "CLICOLOR_FORCE=1", "TERM=xterm-256color")
	for k, v := range p.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting process: %w", err)
	}

	initialStatus := StatusRunning
	if p.ReadyCheck != nil {
		initialStatus = StatusStarting
	}

	p.mu.Lock()
	p.cmd = cmd
	p.pid = cmd.Process.Pid
	p.status = initialStatus
	p.exitCode = 0
	p.reattached = false
	p.logFileHandle = logFile
	p.mu.Unlock()

	program.Send(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: initialStatus})

	p.resetTailDone()
	go p.tailLogFile(program)

	// Start health check polling if configured
	if p.ReadyCheck != nil {
		go p.runReadyCheck(program)
	}

	go func() {
		err := cmd.Wait()

		p.mu.Lock()
		lf := p.logFileHandle
		p.logFileHandle = nil
		p.mu.Unlock()
		if lf != nil {
			lf.Close()
		}

		exitCode := 0
		newStatus := StatusStopped
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			p.mu.Lock()
			if p.status.IsUp() {
				newStatus = StatusCrashed
			}
			p.mu.Unlock()
		}

		p.mu.Lock()
		p.status = newStatus
		p.exitCode = exitCode
		p.cmd = nil
		p.pid = 0
		p.mu.Unlock()

		p.closeTailDone()

		if newStatus == StatusCrashed {
			p.appendSystemLog(fmt.Sprintf("Process exited with code %d", exitCode))
		} else {
			p.appendSystemLog("Process stopped")
		}
		program.Send(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: newStatus, ExitCode: exitCode})
	}()

	return nil
}

func (p *ManagedProcess) runReadyCheck(program *tea.Program) {
	check := p.ReadyCheck
	interval := check.IntervalDuration()
	retries := check.Retries
	if retries <= 0 {
		retries = 30
	}

	p.appendSystemLog(fmt.Sprintf("Waiting for ready check: %s (every %s, %d retries)", check.Command, interval, retries))

	for attempt := 1; attempt <= retries; attempt++ {
		select {
		case <-p.tailDone:
			return
		default:
		}

		p.mu.Lock()
		alive := p.status.IsUp()
		p.mu.Unlock()
		if !alive {
			return
		}

		cmd := exec.Command("sh", "-c", check.Command)
		if p.WorkingDir != "" {
			cmd.Dir = p.WorkingDir
		}
		err := cmd.Run()
		if err == nil {
			// Ready!
			p.mu.Lock()
			if p.status == StatusStarting {
				p.status = StatusRunning
			}
			p.mu.Unlock()
			p.appendSystemLog("Ready check passed")
			program.Send(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: StatusRunning})
			return
		}

		if attempt < retries {
			time.Sleep(interval)
		}
	}

	// All retries exhausted
	p.appendSystemLog(fmt.Sprintf("Ready check failed after %d retries", retries))
	program.Send(AlertMsg{Text: fmt.Sprintf("%s: ready check failed after %d retries", p.Title, retries)})
}

func (p *ManagedProcess) Stop() error {
	p.mu.Lock()
	pid := p.pid
	cmd := p.cmd
	wasReattached := p.reattached
	wasUp := p.status.IsUp()

	// Reset crashed/queued to stopped even if there's nothing to kill
	if p.status == StatusCrashed || p.status == StatusQueued {
		p.status = StatusStopped
		p.mu.Unlock()
		return nil
	}

	if pid == 0 || !wasUp {
		p.mu.Unlock()
		return nil
	}
	p.status = StatusStopped
	p.mu.Unlock()

	if wasReattached {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		go func() {
			time.Sleep(3 * time.Second)
			if state.IsProcessAlive(pid) {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
			p.mu.Lock()
			p.pid = 0
			p.mu.Unlock()
			p.closeTailDone()
		}()
	} else if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		go func() {
			time.Sleep(3 * time.Second)
			p.mu.Lock()
			c := p.cmd
			p.mu.Unlock()
			if c == cmd {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}()
	}

	return nil
}

func (p *ManagedProcess) Restart(program *tea.Program) error {
	if err := p.Stop(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return p.Start(program)
}

func (p *ManagedProcess) tailLogFile(program *tea.Program) {
	f, err := os.Open(p.LogFile)
	if err != nil {
		return
	}
	defer f.Close()

	reader := bufio.NewReader(f)

	for {
		select {
		case <-p.tailDone:
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				p.mu.Lock()
				alive := p.status.IsUp()
				p.mu.Unlock()
				if !alive {
					if line != "" {
						logLine := p.appendLog(line)
						program.Send(LogMsg{ProcessSlug: p.Slug, Group: p.Group, Line: logLine})
					}
					return
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return
		}

		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		logLine := p.appendLog(line)
		program.Send(LogMsg{ProcessSlug: p.Slug, Group: p.Group, Line: logLine})
	}
}

func (p *ManagedProcess) loadLogTail() {
	f, err := os.Open(p.LogFile)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	start := 0
	if len(lines) > maxLogLines {
		start = len(lines) - maxLogLines
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	for i := start; i < len(lines); i++ {
		p.logs = append(p.logs, LogLine{Text: lines[i], Timestamp: time.Now()})
	}
}

func (p *ManagedProcess) monitorPID(program *tea.Program) {
	for {
		select {
		case <-p.tailDone:
			return
		default:
		}

		time.Sleep(1 * time.Second)

		p.mu.Lock()
		pid := p.pid
		alive := p.status.IsUp()
		p.mu.Unlock()

		if !alive || pid == 0 {
			return
		}

		if !state.IsProcessAlive(pid) {
			p.mu.Lock()
			p.status = StatusCrashed
			p.pid = 0
			p.mu.Unlock()

			p.closeTailDone()

			p.appendSystemLog("Process died")
			program.Send(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: StatusCrashed})
			return
		}
	}
}

// SidebarEntry represents a group header, section header, or item in the sidebar.
type SidebarEntry struct {
	IsGroup         bool
	IsSectionHeader bool
	SectionTitle    string
	Group           string
	Item            SidebarItem
	Hidden          bool // task is hidden by user but showHiddenTasks is on
}

type Manager struct {
	Processes []*ManagedProcess
	Tasks     []*ManagedTask
	Items     []SidebarItem
	Groups    []string
	Sidebar   []SidebarEntry
	byKey     map[string]*ManagedProcess
	RootDir   string
	Program   *tea.Program
}

func NewManager(rootDir string) *Manager {
	return &Manager{
		RootDir: rootDir,
		byKey:   make(map[string]*ManagedProcess),
	}
}

func (m *Manager) Add(proc *ManagedProcess) {
	m.Processes = append(m.Processes, proc)
	m.Items = append(m.Items, proc)
	key := proc.Group + "/" + proc.Slug
	m.byKey[key] = proc
}

func (m *Manager) AddTask(task *ManagedTask) {
	m.Tasks = append(m.Tasks, task)
	m.Items = append(m.Items, task)
}

func (m *Manager) Get(group, slug string) *ManagedProcess {
	return m.byKey[group+"/"+slug]
}

func (m *Manager) BuildSidebar(collapsedGroups map[string]bool, hiddenTasks map[string]bool, showHiddenTasks bool) {
	m.Sidebar = nil
	seenGroups := make(map[string]bool)
	var orderedGroups []string

	for _, item := range m.Items {
		if !seenGroups[item.GetGroup()] {
			seenGroups[item.GetGroup()] = true
			orderedGroups = append(orderedGroups, item.GetGroup())
		}
	}
	m.Groups = orderedGroups

	multiGroup := len(orderedGroups) > 1

	for _, group := range orderedGroups {
		if multiGroup {
			m.Sidebar = append(m.Sidebar, SidebarEntry{IsGroup: true, Group: group})
		}
		if collapsedGroups[group] {
			continue
		}

		// Check if this group has both processes and tasks (for section header).
		hasProcesses := false
		for _, item := range m.Items {
			if item.GetGroup() == group && item.Kind() == KindProcess {
				hasProcesses = true
				break
			}
		}

		addedTaskHeader := false
		for _, item := range m.Items {
			if item.GetGroup() != group {
				continue
			}
			if item.Kind() == KindTask {
				if !addedTaskHeader {
					if hasProcesses {
						m.Sidebar = append(m.Sidebar, SidebarEntry{
							IsSectionHeader: true,
							SectionTitle:    "tasks",
							Group:           group,
						})
					}
					addedTaskHeader = true
				}
				taskKey := group + "/" + item.GetSlug()
				if hiddenTasks[taskKey] && !showHiddenTasks {
					continue
				}
				m.Sidebar = append(m.Sidebar, SidebarEntry{
					Group:  group,
					Item:   item,
					Hidden: hiddenTasks[taskKey],
				})
			} else {
				m.Sidebar = append(m.Sidebar, SidebarEntry{Group: group, Item: item})
			}
		}
	}
}

// SelectableIndices returns indices of all selectable sidebar entries (groups and items, not section headers).
func (m *Manager) SelectableIndices() []int {
	var indices []int
	for i, entry := range m.Sidebar {
		if !entry.IsSectionHeader {
			indices = append(indices, i)
		}
	}
	return indices
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

// StartGroup starts all items in a group — processes via dependency-ordered batch, tasks directly.
func (m *Manager) StartGroup(group string) {
	for _, task := range m.Tasks {
		if task.Group == group && !task.GetStatus().IsUp() {
			_ = task.Start(m.Program)
		}
	}
	m.StartBatch(m.ProcessesInGroup(group))
}

// CheckDependencies returns a list of unmet dependency descriptions.
// A dependency is met if the target process is StatusRunning (not just StatusStarting).
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

func parseDependency(dep string, sameGroup string) (string, string) {
	if idx := strings.Index(dep, ":"); idx != -1 {
		return dep[:idx], dep[idx+1:]
	}
	return sameGroup, dep
}

// StartWithDependencyCheck starts a process if dependencies are met.
// Returns alert text on failure, empty string on success.
func (m *Manager) StartWithDependencyCheck(proc *ManagedProcess) string {
	unmet := m.CheckDependencies(proc)
	if len(unmet) > 0 {
		return fmt.Sprintf("Cannot start %s: dependencies not running: %s", proc.Title, strings.Join(unmet, ", "))
	}
	if err := proc.Start(m.Program); err != nil {
		return fmt.Sprintf("Failed to start %s: %s", proc.Title, err)
	}
	return ""
}

// StartBatch starts a set of processes respecting dependency order.
// Processes with no unmet dependencies start immediately in parallel.
// As each process reaches StatusRunning (health check passed), any pending
// processes whose dependencies are now all met get started.
// This continues until all processes are started or remaining ones have
// unresolvable dependencies.
func (m *Manager) StartBatch(processes []*ManagedProcess) {
	if len(processes) == 0 {
		return
	}

	// Build the set of processes we need to start
	pending := make(map[string]*ManagedProcess) // key -> process
	for _, proc := range processes {
		status := proc.Status()
		if !status.IsUp() && status != StatusQueued {
			key := proc.Group + "/" + proc.Slug
			pending[key] = proc
			proc.SetQueued(m.Program)
		}
	}

	if len(pending) == 0 {
		return
	}

	// Subscribe to status changes via polling
	// We check every 500ms which pending processes can now start
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Timeout after 5 minutes to avoid hanging forever
	deadline := time.After(5 * time.Minute)

	// Track what we've already kicked off
	started := make(map[string]bool)

	for {
		if len(pending) == 0 {
			return
		}

		// Try to start any process whose dependencies are all met
		startedThisRound := false
		for key, proc := range pending {
			if started[key] {
				// Already started, check if it's running now
				if proc.Status() == StatusRunning {
					delete(pending, key)
					startedThisRound = true
				} else if proc.Status() == StatusCrashed || proc.Status() == StatusStopped {
					// Failed — remove from pending, don't block others
					delete(pending, key)
					startedThisRound = true
				}
				continue
			}

			unmet := m.CheckDependencies(proc)
			if len(unmet) == 0 {
				if err := proc.Start(m.Program); err != nil {
					proc.appendSystemLog(fmt.Sprintf("Failed to start: %s", err))
					delete(pending, key)
				} else {
					started[key] = true
					// If no ready check, it's immediately running — remove
					if proc.ReadyCheck == nil {
						delete(pending, key)
					}
				}
				startedThisRound = true
			}
		}

		// If nothing is pending, we're done
		if len(pending) == 0 {
			return
		}

		// Check if we're stuck: everything pending is either waiting for a
		// started process to become ready, or has unresolvable deps
		allBlocked := true
		for key, proc := range pending {
			if started[key] {
				// Waiting for ready check — not stuck
				allBlocked = false
				continue
			}
			// Check if any dependency is in the started set (still becoming ready)
			for _, dep := range proc.DependsOn {
				depGroup, depSlug := parseDependency(dep, proc.Group)
				depKey := depGroup + "/" + depSlug
				if started[depKey] {
					allBlocked = false
					break
				}
			}
		}

		if allBlocked && !startedThisRound {
			for key, proc := range pending {
				if !started[key] {
					unmet := m.CheckDependencies(proc)
					proc.appendSystemLog(fmt.Sprintf("Cannot start: dependencies not running: %s", strings.Join(unmet, ", ")))
					proc.mu.Lock()
					proc.status = StatusStopped
					proc.mu.Unlock()
					m.Program.Send(StatusChangeMsg{ProcessSlug: proc.Slug, Group: proc.Group, Status: StatusStopped})
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
					proc.appendSystemLog("Timed out waiting for dependencies")
					proc.mu.Lock()
					proc.status = StatusStopped
					proc.mu.Unlock()
					m.Program.Send(StatusChangeMsg{ProcessSlug: proc.Slug, Group: proc.Group, Status: StatusStopped})
				}
			}
			return
		}
	}
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

func (m *Manager) ReattachFromState(session *state.SessionState) int {
	reattached := 0
	for _, proc := range m.Processes {
		key := state.ProcessKey(proc.Group, proc.Slug)
		saved, exists := session.Processes[key]
		if !exists {
			continue
		}
		if !state.IsProcessAlive(saved.PID) {
			continue
		}
		proc.Reattach(saved.PID, m.Program)
		reattached++
	}
	return reattached
}

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
		key := state.ProcessKey(proc.Group, proc.Slug)
		session.Processes[key] = state.ProcessState{
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

func (m *Manager) Summary() string {
	running := 0
	for _, item := range m.Items {
		if item.GetStatus().IsUp() {
			running++
		}
	}
	return fmt.Sprintf("%d/%d running", running, len(m.Items))
}

func (m *Manager) RunningInGroup(group string) string {
	running := 0
	total := 0
	for _, item := range m.Items {
		if item.GetGroup() == group {
			total++
			if item.GetStatus().IsUp() {
				running++
			}
		}
	}
	return fmt.Sprintf("%d/%d", running, total)
}
