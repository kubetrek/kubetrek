//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// detach puts the agent in its own session, so closing the terminal doesn't stop it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// ponytail: a reused pid looks alive; checking the process name would rule that out.
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}

func stop(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
