package process

import (
	"os"
	"os/exec"
	"syscall"
)

// buildCmd creates a shell command with standard process group isolation and
// environment settings (color forcing, terminal type). I/O redirection is left
// to the caller.
func buildCmd(command, workingDir string, env map[string]string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "FORCE_COLOR=1", "CLICOLOR_FORCE=1", "TERM=xterm-256color")
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd
}

// isAlive checks whether a process with the given PID is still running.
// On Unix, os.FindProcess always succeeds; signal 0 is used to probe liveness.
func isAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
