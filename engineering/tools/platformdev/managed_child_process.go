//go:build darwin || linux

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// detachManagedRuntime 使后台编排器拥有独立会话，不再受启动 PTY
// 的 SIGHUP 影响。它必须在启动任何受管子进程前执行。
func detachManagedRuntime() error {
	if _, err := syscall.Setsid(); err != nil {
		return fmt.Errorf("setsid: %w", err)
	}
	return nil
}

// configureManagedChild 把长期运行的受管子进程与启动终端的进程组隔离。
// platformdev 仍是它们的生命周期所有者，但 shell/PTY 退出不再把 SIGHUP
// 越过编排器直接发给 Portal Kernel 或 Backend Kernel。
func configureManagedChild(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
