package protocolbus

import (
	"os"
	"os/exec"
	"reflect"
	"testing"
)

type recordingSessionGuardian struct{ calls []string }

func (g *recordingSessionGuardian) Prepare(*exec.Cmd) error {
	g.calls = append(g.calls, "prepare")
	return nil
}

func (g *recordingSessionGuardian) Terminate(*exec.Cmd) error {
	g.calls = append(g.calls, "terminate")
	return nil
}

func (g *recordingSessionGuardian) Kill(*exec.Cmd) error {
	g.calls = append(g.calls, "kill")
	return nil
}

func TestKillProcessSweepsGroupAfterLeaderAlreadyExited(t *testing.T) {
	processDone := make(chan struct{})
	close(processDone)
	guardian := &recordingSessionGuardian{}
	sess := &session{
		cmd:         &exec.Cmd{Process: &os.Process{Pid: 4242}},
		processDone: processDone,
		guardian:    guardian,
	}
	sess.killProcess()
	if expected := []string{"terminate", "kill"}; !reflect.DeepEqual(guardian.calls, expected) {
		t.Fatalf("组长退出后仍必须最终清扫进程组: got=%v want=%v", guardian.calls, expected)
	}
}
