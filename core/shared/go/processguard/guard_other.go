//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package processguard

import "os/exec"

type platformGuardian struct{}

func (platformGuardian) Prepare(command *exec.Cmd) error {
	return validateCommand(command)
}

func (platformGuardian) Terminate(command *exec.Cmd) error {
	if err := validateCommand(command); err != nil {
		return err
	}
	if command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}

func (platformGuardian) Kill(command *exec.Cmd) error {
	return platformGuardian{}.Terminate(command)
}
