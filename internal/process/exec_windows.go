//go:build windows

package process

import (
	"os"
	"os/exec"
)

func buildCmd(command, workingDir string, env map[string]string) *exec.Cmd {
	cmd := exec.Command("cmd", "/C", command)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "FORCE_COLOR=1", "CLICOLOR_FORCE=1")
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd
}

func isAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	return err == nil && proc != nil
}
