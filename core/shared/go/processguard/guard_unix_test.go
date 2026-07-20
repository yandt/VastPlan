//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package processguard

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestPlatformGuardianCreatesAndTerminatesProcessGroup(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestProcessGuardBlockingHelper")
	command.Env = append(os.Environ(), "VASTPLAN_PROCESS_GUARD_HELPER=1")
	guardian := Default()
	if err := guardian.Prepare(command); err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() { _ = guardian.Kill(command) })
	groupID, err := syscall.Getpgid(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if groupID != command.Process.Pid {
		t.Fatalf("子进程必须成为独立进程组组长: pid=%d pgid=%d", command.Process.Pid, groupID)
	}
	if err := guardian.Terminate(command); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("进程组未响应终止信号")
	}
}

func TestProcessGuardBlockingHelper(t *testing.T) {
	if os.Getenv("VASTPLAN_PROCESS_GUARD_HELPER") != "1" {
		return
	}
	select {}
}
