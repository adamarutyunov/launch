//go:build windows

package process

import "os"

func killProcessGroup(pid int, terminate bool) {
	p, err := os.FindProcess(pid)
	if err == nil {
		_ = p.Kill()
	}
}
