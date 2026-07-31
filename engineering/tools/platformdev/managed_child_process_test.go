//go:build darwin || linux

package main

import (
	"os/exec"
	"testing"
)

func TestConfigureManagedChildSeparatesTerminalProcessGroup(t *testing.T) {
	command := exec.Command("true")
	configureManagedChild(command)
	if command.SysProcAttr == nil || !command.SysProcAttr.Setpgid {
		t.Fatal("受管子进程必须脱离启动终端的进程组，避免后台启动后收到 SIGHUP")
	}
}
