//go:build linux

package processguard

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLinuxParentDeathTerminatesGuardedChild(t *testing.T) {
	pidFile := t.TempDir() + "/child.pid"
	parent := exec.Command(os.Args[0], "-test.run=TestLinuxProcessGuardParentHelper")
	parent.Env = append(os.Environ(),
		"VASTPLAN_PROCESS_GUARD_PARENT_HELPER=1", "VASTPLAN_PROCESS_GUARD_PID_FILE="+pidFile)
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	childPID := waitForChildPID(t, pidFile)
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = parent.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if processExitedOrZombie(childPID) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("PDEATHSIG 未在父进程死亡后终止子进程 pid=%d", childPID)
}

func TestLinuxProcessGuardParentHelper(t *testing.T) {
	if os.Getenv("VASTPLAN_PROCESS_GUARD_PARENT_HELPER") != "1" {
		return
	}
	child := exec.Command(os.Args[0], "-test.run=TestLinuxProcessGuardChildHelper")
	child.Env = append(os.Environ(), "VASTPLAN_PROCESS_GUARD_CHILD_HELPER=1")
	guardian := Default()
	if err := guardian.Prepare(child); err != nil {
		panic(err)
	}
	if err := child.Start(); err != nil {
		panic(err)
	}
	if err := os.WriteFile(os.Getenv("VASTPLAN_PROCESS_GUARD_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		panic(err)
	}
	select {}
}

func TestLinuxProcessGuardChildHelper(t *testing.T) {
	if os.Getenv("VASTPLAN_PROCESS_GUARD_CHILD_HELPER") != "1" {
		return
	}
	select {}
}

func waitForChildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if parseErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("未收到受守护子进程 PID")
	return 0
}

func processExitedOrZombie(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return true
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	fields := strings.Fields(string(raw))
	return len(fields) > 2 && fields[2] == "Z"
}
