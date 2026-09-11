//go:build !windows

package ipc

import (
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return nil
}
