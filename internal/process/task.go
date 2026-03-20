package process

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type ManagedTask struct {
	Slug        string
	Description string // human-readable description from Taskfile desc field; may be empty
	Group       string
	Command     string
	WorkingDir  string

	mu       sync.Mutex
	cmd      *exec.Cmd
	status   Status
	exitCode int
	logs     []LogLine
}

func NewManagedTask(slug, description, group, command, workingDir string) *ManagedTask {
	return &ManagedTask{
		Slug:        slug,
		Description: description,
		Group:       group,
		Command:     command,
		WorkingDir:  workingDir,
		status:      StatusStopped,
		logs:        make([]LogLine, 0, 64),
	}
}

func (t *ManagedTask) GetSlug() string  { return t.Slug }
func (t *ManagedTask) GetTitle() string { return t.Slug } // sidebar always shows the slug
func (t *ManagedTask) GetGroup() string { return t.Group }
func (t *ManagedTask) Kind() ItemKind   { return KindTask }

func (t *ManagedTask) GetStatus() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

func (t *ManagedTask) ExitCode() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exitCode
}

func (t *ManagedTask) GetLogs() []LogLine {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]LogLine, len(t.logs))
	copy(result, t.logs)
	return result
}

func (t *ManagedTask) ClearLogs() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logs = t.logs[:0]
}

func (t *ManagedTask) appendLog(text string) LogLine {
	line := LogLine{Text: text, Timestamp: time.Now()}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logs = append(t.logs, line)
	if len(t.logs) > maxLogLines {
		t.logs = t.logs[len(t.logs)-maxLogLines:]
	}
	return line
}

func (t *ManagedTask) appendSystemLog(text string) LogLine {
	line := LogLine{Text: text, Timestamp: time.Now(), IsSystem: true}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logs = append(t.logs, line)
	if len(t.logs) > maxLogLines {
		t.logs = t.logs[len(t.logs)-maxLogLines:]
	}
	return line
}

func (t *ManagedTask) Start(program *tea.Program) error {
	t.mu.Lock()
	if t.status == StatusRunning {
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()

	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("creating pipe: %w", err)
	}

	cmd := exec.Command("sh", "-c", t.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if t.WorkingDir != "" {
		cmd.Dir = t.WorkingDir
	}
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "FORCE_COLOR=1", "CLICOLOR_FORCE=1", "TERM=xterm-256color")
	cmd.Stdout = w
	cmd.Stderr = w

	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return fmt.Errorf("starting task: %w", err)
	}

	t.mu.Lock()
	t.cmd = cmd
	t.status = StatusRunning
	t.exitCode = 0
	t.mu.Unlock()

	program.Send(StatusChangeMsg{ProcessSlug: t.Slug, Group: t.Group, Status: StatusRunning})

	// exitCh is buffered so the waiter goroutine doesn't block before closing w.
	exitCh := make(chan int, 1)

	go func() {
		err := cmd.Wait()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		exitCh <- exitCode
		w.Close() // triggers EOF in the reader goroutine
	}()

	go func() {
		defer r.Close()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := t.appendLog(scanner.Text())
			program.Send(LogMsg{ProcessSlug: t.Slug, Group: t.Group, Line: line})
		}

		exitCode := <-exitCh

		t.mu.Lock()
		currentStatus := t.status
		if currentStatus == StatusRunning {
			// Natural exit — determine success or failure.
			if exitCode == 0 {
				t.status = StatusSucceeded
			} else {
				t.status = StatusFailed
				t.exitCode = exitCode
			}
		}
		// If status was already set to StatusStopped by Stop(), leave it.
		t.cmd = nil
		newStatus := t.status
		t.mu.Unlock()

		var systemMsg string
		switch newStatus {
		case StatusSucceeded:
			systemMsg = "Task finished"
		case StatusFailed:
			systemMsg = fmt.Sprintf("Task failed (exit %d)", exitCode)
		default:
			systemMsg = "Task stopped"
		}
		line := t.appendSystemLog(systemMsg)
		program.Send(LogMsg{ProcessSlug: t.Slug, Group: t.Group, Line: line})
		program.Send(StatusChangeMsg{ProcessSlug: t.Slug, Group: t.Group, Status: newStatus, ExitCode: exitCode})
	}()

	return nil
}

func (t *ManagedTask) Stop() error {
	t.mu.Lock()
	cmd := t.cmd
	if cmd == nil || t.status != StatusRunning {
		t.mu.Unlock()
		return nil
	}
	t.status = StatusStopped
	t.mu.Unlock()

	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		go func() {
			time.Sleep(2 * time.Second)
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}()
	}
	return nil
}
