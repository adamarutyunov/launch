package process

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ManagedTask is a one-shot command (from a Taskfile) that runs to completion.
type ManagedTask struct {
	Slug       string
	Subtitle   string // human-readable description from Taskfile desc field; may be empty
	Group      string
	Command    string
	WorkingDir string

	logBuf   logBuffer
	notifier Notifier
	mu       sync.Mutex // protects cmd / status / exitCode
	cmd      *exec.Cmd
	status   Status
	exitCode int
}

func NewManagedTask(slug, subtitle, group, command, workingDir string) *ManagedTask {
	return &ManagedTask{
		Slug:       slug,
		Subtitle:   subtitle,
		Group:      group,
		Command:    command,
		WorkingDir: workingDir,
		status:     StatusStopped,
	}
}

// notify sends an event through the registered notifier, if any.
func (t *ManagedTask) notify(msg any) {
	if t.notifier != nil {
		t.notifier.Send(msg)
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

// GetLogs implements SidebarItem.
func (t *ManagedTask) GetLogs() []LogLine { return t.logBuf.snapshot() }

// ClearLogs implements SidebarItem.
func (t *ManagedTask) ClearLogs() { t.logBuf.clear() }

// Start implements SidebarItem.
func (t *ManagedTask) Start() error {
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

	cmd := buildCmd(t.Command, t.WorkingDir, nil)
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

	t.notify(StatusChangeMsg{ProcessSlug: t.Slug, Group: t.Group, Status: StatusRunning})

	// exitCh is buffered so the waiter goroutine doesn't block before closing w.
	exitCh := make(chan int, 1)

	go func() {
		waitErr := cmd.Wait()
		exitCode := 0
		if waitErr != nil {
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
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
			line := t.logBuf.write(scanner.Text(), false)
			t.notify(LogMsg{ProcessSlug: t.Slug, Group: t.Group, Line: line})
		}

		exitCode := <-exitCh

		t.mu.Lock()
		if t.status == StatusRunning {
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
		line := t.logBuf.write(systemMsg, true)
		t.notify(LogMsg{ProcessSlug: t.Slug, Group: t.Group, Line: line})
		t.notify(StatusChangeMsg{ProcessSlug: t.Slug, Group: t.Group, Status: newStatus, ExitCode: exitCode})
	}()

	return nil
}

// Stop implements SidebarItem.
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
