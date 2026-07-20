package nodeagent

import (
	"os"
	"os/exec"
	"reflect"
	"testing"
)

type recordingRuntimeGuardian struct{ calls []string }

func (g *recordingRuntimeGuardian) Prepare(*exec.Cmd) error {
	g.calls = append(g.calls, "prepare")
	return nil
}

func (g *recordingRuntimeGuardian) Terminate(*exec.Cmd) error {
	g.calls = append(g.calls, "terminate")
	return nil
}

func (g *recordingRuntimeGuardian) Kill(*exec.Cmd) error {
	g.calls = append(g.calls, "kill")
	return nil
}

func TestRuntimeHostShutdownSweepsGroupAfterLeaderAlreadyExited(t *testing.T) {
	done := make(chan struct{})
	close(done)
	guardian := &recordingRuntimeGuardian{}
	host := &runtimeHostProcess{
		cmd:      &exec.Cmd{Process: &os.Process{Pid: 4242}},
		stdin:    nopWriteCloser{},
		pending:  map[string]chan runtimeControlResponse{},
		done:     done,
		guardian: guardian,
	}
	host.shutdown()
	if expected := []string{"kill"}; !reflect.DeepEqual(guardian.calls, expected) {
		t.Fatalf("Runtime Host 组长退出后仍必须清扫进程组: got=%v want=%v", guardian.calls, expected)
	}
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(payload []byte) (int, error) { return len(payload), nil }
func (nopWriteCloser) Close() error                      { return nil }
