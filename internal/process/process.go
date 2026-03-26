package process

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/adamarutyunov/launch/internal/config"
)

// ManagedProcess is a long-running service with optional health checks and
// dependency ordering.
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

	logBuf        logBuffer
	notifier      Notifier
	mu            sync.Mutex // protects the fields below
	cmd           *exec.Cmd
	pid           int
	reattached    bool
	status        Status
	exitCode      int
	logFileHandle *os.File
	tailDone      chan struct{}
	tailDoneOnce  sync.Once
}

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
	}
}

// notify sends an event through the registered notifier, if any.
func (p *ManagedProcess) notify(msg any) {
	if p.notifier != nil {
		p.notifier.Send(msg)
	}
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
func (p *ManagedProcess) GetLogs() []LogLine { return p.logBuf.snapshot() }

// ClearLogs implements SidebarItem.
func (p *ManagedProcess) ClearLogs() { p.logBuf.clear() }

// Kind implements SidebarItem.
func (p *ManagedProcess) Kind() ItemKind { return KindProcess }

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

func (p *ManagedProcess) SetQueued() {
	p.mu.Lock()
	p.status = StatusQueued
	p.mu.Unlock()
	p.notify(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: StatusQueued})
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

func (p *ManagedProcess) Reattach(pid int) {
	p.mu.Lock()
	p.pid = pid
	p.status = StatusRunning
	p.reattached = true
	p.mu.Unlock()

	p.loadLogTail()

	p.resetTailDone()
	go p.tailLogFile()
	go p.monitorPID()

	p.notify(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: StatusRunning})
}

// Start implements SidebarItem.
func (p *ManagedProcess) Start() error {
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

	cmd := buildCmd(p.Command, p.WorkingDir, p.Env)
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

	p.notify(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: initialStatus})

	p.resetTailDone()
	go p.tailLogFile()

	if p.ReadyCheck != nil {
		go p.runReadyCheck()
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
			p.logBuf.write(fmt.Sprintf("Process exited with code %d", exitCode), true)
		} else {
			p.logBuf.write("Process stopped", true)
		}
		p.notify(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: newStatus, ExitCode: exitCode})
	}()

	return nil
}

func (p *ManagedProcess) runReadyCheck() {
	check := p.ReadyCheck
	interval := check.IntervalDuration()
	retries := check.Retries
	if retries <= 0 {
		retries = 30
	}

	p.logBuf.write(fmt.Sprintf("Waiting for ready check: %s (every %s, %d retries)", check.Command, interval, retries), true)

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
		if err := cmd.Run(); err == nil {
			p.mu.Lock()
			if p.status == StatusStarting {
				p.status = StatusRunning
			}
			p.mu.Unlock()
			p.logBuf.write("Ready check passed", true)
			p.notify(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: StatusRunning})
			return
		}

		if attempt < retries {
			time.Sleep(interval)
		}
	}

	p.logBuf.write(fmt.Sprintf("Ready check failed after %d retries", retries), true)
	p.notify(AlertMsg{Text: fmt.Sprintf("%s: ready check failed after %d retries", p.Title, retries)})
}

// Stop implements SidebarItem.
func (p *ManagedProcess) Stop() error {
	p.mu.Lock()
	pid := p.pid
	cmd := p.cmd
	wasReattached := p.reattached
	wasUp := p.status.IsUp()

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
		killProcessGroup(pid, true)
		go func() {
			time.Sleep(3 * time.Second)
			if isAlive(pid) {
				killProcessGroup(pid, false)
			}
			p.mu.Lock()
			p.pid = 0
			p.mu.Unlock()
			p.closeTailDone()
		}()
	} else if cmd != nil && cmd.Process != nil {
		killProcessGroup(cmd.Process.Pid, true)
		go func() {
			time.Sleep(3 * time.Second)
			p.mu.Lock()
			c := p.cmd
			p.mu.Unlock()
			if c == cmd {
				killProcessGroup(cmd.Process.Pid, false)
			}
		}()
	}

	return nil
}

func (p *ManagedProcess) Restart() error {
	if err := p.Stop(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return p.Start()
}

func (p *ManagedProcess) tailLogFile() {
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
						logLine := p.logBuf.write(line, false)
						p.notify(LogMsg{ProcessSlug: p.Slug, Group: p.Group, Line: logLine})
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

		logLine := p.logBuf.write(line, false)
		p.notify(LogMsg{ProcessSlug: p.Slug, Group: p.Group, Line: logLine})
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

	if len(lines) > maxLogLines {
		lines = lines[len(lines)-maxLogLines:]
	}

	p.logBuf.load(lines)
}

func (p *ManagedProcess) monitorPID() {
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

		if !isAlive(pid) {
			p.mu.Lock()
			p.status = StatusCrashed
			p.pid = 0
			p.mu.Unlock()

			p.closeTailDone()

			p.logBuf.write("Process died", true)
			p.notify(StatusChangeMsg{ProcessSlug: p.Slug, Group: p.Group, Status: StatusCrashed})
			return
		}
	}
}
