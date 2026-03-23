//go:build !windows

package process

import "syscall"

func killProcessGroup(pid int, terminate bool) {
	sig := syscall.SIGKILL
	if terminate {
		sig = syscall.SIGTERM
	}
	_ = syscall.Kill(-pid, sig)
}
