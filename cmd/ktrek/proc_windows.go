package main

import (
	"os"
	"os/exec"
	"syscall"
)

const detachedProcess = 0x00000008

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP}
}

// FindProcess opens a handle on Windows, so it fails once the process is gone.
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err == nil {
		p.Release()
	}
	return err == nil
}

func stop(p *os.Process) error {
	return p.Kill()
}
