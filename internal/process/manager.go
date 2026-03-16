package process

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type Status int

const (
	StatusStopped Status = iota
	StatusRunning
	StatusCrashed
)

func (s Status) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusCrashed:
		return "crashed"
	default:
		return "stopped"
	}
}

const maxLogLines = 10000

type LogLine struct {
	Text      string
	Timestamp time.Time
}

type ManagedProcess struct {
	Name       string
	Group      string
	Command    string
	WorkingDir string
	Env        map[string]string
	AutoStart  bool

	mu       sync.Mutex
	cmd      *exec.Cmd
	status   Status
	logs     []LogLine
	exitCode int
}

// Messages sent to bubbletea
type LogMsg struct {
	ProcessName string
	Group       string
	Line        LogLine
}

type StatusChangeMsg struct {
	ProcessName string
	Group       string
	Status      Status
	ExitCode    int
}

func NewManagedProcess(name, group, command, workingDir string, env map[string]string, autoStart bool) *ManagedProcess {
	return &ManagedProcess{
		Name:       name,
		Group:      group,
		Command:    command,
		WorkingDir: workingDir,
		Env:        env,
		AutoStart:  autoStart,
		status:     StatusStopped,
		logs:       make([]LogLine, 0, 256),
	}
}

func (p *ManagedProcess) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
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

func (p *ManagedProcess) ClearLogs() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logs = p.logs[:0]
}

func (p *ManagedProcess) Start(program *tea.Program) error {
	p.mu.Lock()
	if p.status == StatusRunning {
		p.mu.Unlock()
		return fmt.Errorf("process %s is already running", p.Name)
	}

	cmd := exec.Command("sh", "-c", p.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if p.WorkingDir != "" {
		cmd.Dir = p.WorkingDir
	}
	cmd.Env = os.Environ()
	for k, v := range p.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("starting process: %w", err)
	}

	p.cmd = cmd
	p.status = StatusRunning
	p.exitCode = 0
	p.mu.Unlock()

	program.Send(StatusChangeMsg{ProcessName: p.Name, Group: p.Group, Status: StatusRunning})

	go p.streamOutput(stdout, program)

	go func() {
		err := cmd.Wait()
		exitCode := 0
		status := StatusStopped
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			p.mu.Lock()
			if p.status == StatusRunning {
				status = StatusCrashed
			}
			p.mu.Unlock()
		}

		p.mu.Lock()
		p.status = status
		p.exitCode = exitCode
		p.cmd = nil
		p.mu.Unlock()

		if status == StatusCrashed {
			p.appendLog(fmt.Sprintf("--- process exited with code %d ---", exitCode))
		} else {
			p.appendLog("--- process stopped ---")
		}
		program.Send(StatusChangeMsg{ProcessName: p.Name, Group: p.Group, Status: status, ExitCode: exitCode})
	}()

	return nil
}

func (p *ManagedProcess) Stop() error {
	p.mu.Lock()
	cmd := p.cmd
	if cmd == nil || p.status != StatusRunning {
		p.mu.Unlock()
		return nil
	}
	p.status = StatusStopped
	p.mu.Unlock()

	if cmd.Process != nil {
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

func (p *ManagedProcess) streamOutput(reader io.Reader, program *tea.Program) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		text := scanner.Text()
		line := p.appendLog(text)
		program.Send(LogMsg{ProcessName: p.Name, Group: p.Group, Line: line})
	}
}

// SidebarEntry represents either a group header or a process in the sidebar.
type SidebarEntry struct {
	IsGroup bool
	Group   string
	Process *ManagedProcess // nil for group headers
}

type Manager struct {
	Processes []*ManagedProcess
	Groups    []string // ordered group names
	Sidebar   []SidebarEntry
	byName    map[string]*ManagedProcess

	// Program reference, set before Run()
	Program *tea.Program
}

func NewManager() *Manager {
	return &Manager{
		byName: make(map[string]*ManagedProcess),
	}
}

func (m *Manager) Add(proc *ManagedProcess) {
	m.Processes = append(m.Processes, proc)
	key := proc.Group + "/" + proc.Name
	m.byName[key] = proc
}

func (m *Manager) BuildSidebar() {
	m.Sidebar = nil
	seenGroups := make(map[string]bool)
	var orderedGroups []string

	for _, proc := range m.Processes {
		if !seenGroups[proc.Group] {
			seenGroups[proc.Group] = true
			orderedGroups = append(orderedGroups, proc.Group)
		}
	}
	m.Groups = orderedGroups

	multiGroup := len(orderedGroups) > 1

	for _, group := range orderedGroups {
		if multiGroup {
			m.Sidebar = append(m.Sidebar, SidebarEntry{IsGroup: true, Group: group})
		}
		for _, proc := range m.Processes {
			if proc.Group == group {
				m.Sidebar = append(m.Sidebar, SidebarEntry{Group: group, Process: proc})
			}
		}
	}
}

// SelectableEntries returns indices of sidebar entries that are processes (not group headers).
func (m *Manager) SelectableIndices() []int {
	var indices []int
	for i, entry := range m.Sidebar {
		if !entry.IsGroup {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m *Manager) StartAll() {
	for _, proc := range m.Processes {
		if proc.AutoStart {
			if err := proc.Start(m.Program); err != nil {
				proc.appendLog(fmt.Sprintf("--- failed to start: %s ---", err))
			}
		}
	}
}

func (m *Manager) StopAll() {
	var wg sync.WaitGroup
	for _, proc := range m.Processes {
		wg.Add(1)
		go func(p *ManagedProcess) {
			defer wg.Done()
			_ = p.Stop()
		}(proc)
	}
	wg.Wait()
}

func (m *Manager) Summary() string {
	running := 0
	for _, proc := range m.Processes {
		if proc.Status() == StatusRunning {
			running++
		}
	}
	return fmt.Sprintf("%d/%d running", running, len(m.Processes))
}

func (m *Manager) RunningInGroup(group string) string {
	running := 0
	total := 0
	for _, proc := range m.Processes {
		if proc.Group == group {
			total++
			if proc.Status() == StatusRunning {
				running++
			}
		}
	}
	return fmt.Sprintf("%d/%d", running, total)
}

