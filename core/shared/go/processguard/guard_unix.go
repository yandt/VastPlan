//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package processguard

import (
	"errors"
	"os/exec"
	"syscall"
)

type platformGuardian struct{}

func (platformGuardian) Prepare(command *exec.Cmd) error {
	if err := validateCommand(command); err != nil {
		return err
	}
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.Setpgid = true
	return nil
}

func (platformGuardian) Terminate(command *exec.Cmd) error {
	return signalProcessGroup(command, syscall.SIGTERM)
}

func (platformGuardian) Kill(command *exec.Cmd) error {
	return signalProcessGroup(command, syscall.SIGKILL)
}

func signalProcessGroup(command *exec.Cmd, signal syscall.Signal) error {
	if err := validateCommand(command); err != nil {
		return err
	}
	if command.Process == nil {
		return nil
	}
	err := syscall.Kill(-command.Process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
