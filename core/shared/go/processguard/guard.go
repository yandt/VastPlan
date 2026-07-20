// Package processguard centralizes operating-system process lifecycle policy
// for independently launched plugins and shared Runtime Hosts.
package processguard

import (
	"errors"
	"os/exec"
)

// Guardian prepares a direct child before Start and later signals the complete
// process group. Implementations must not invoke a shell or rewrite argv.
type Guardian interface {
	Prepare(*exec.Cmd) error
	Terminate(*exec.Cmd) error
	Kill(*exec.Cmd) error
}

// Default returns the platform guardian. Kernel code accepts Guardian as an
// interface for deterministic tests, while production always uses this value.
func Default() Guardian { return platformGuardian{} }

func validateCommand(command *exec.Cmd) error {
	if command == nil {
		return errors.New("process guardian command 不能为 nil")
	}
	return nil
}
