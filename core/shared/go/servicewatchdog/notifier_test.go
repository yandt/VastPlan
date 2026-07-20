package servicewatchdog

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNotifierSendsReadyAndWatchdogOnlyForLiveAgent(t *testing.T) {
	messages := make(chan string, 4)
	notifier := &Notifier{socket: "/test/notify.sock", watchdogTimeout: 600 * time.Millisecond,
		send: func(message string) error { messages <- message; return nil }}
	if !notifier.Enabled() || notifier.WatchdogTimeout() != 600*time.Millisecond {
		t.Fatalf("systemd 通知配置未加载: %+v", notifier)
	}
	if err := notifier.Ready("Node Agent ready\nnow"); err != nil {
		t.Fatal(err)
	}
	if message := readNotification(t, messages, time.Second); !strings.Contains(message, "READY=1") || !strings.Contains(message, "STATUS=Node Agent ready now") {
		t.Fatalf("就绪通知无效: %q", message)
	}

	liveness := &Liveness{}
	liveness.Pulse()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notifier.Start(ctx, liveness, nil)
	if message := readNotification(t, messages, time.Second); message != "WATCHDOG=1" {
		t.Fatalf("watchdog 通知无效: %q", message)
	}
}

func TestNotifierIgnoresWatchdogForDifferentPID(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "/tmp/vastplan-notify-unused.sock")
	t.Setenv("WATCHDOG_PID", "999999")
	t.Setenv("WATCHDOG_USEC", "1000000")
	notifier, err := FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if notifier.WatchdogTimeout() != 0 {
		t.Fatalf("其他进程的 watchdog 不得被当前内核使用: %v", notifier.WatchdogTimeout())
	}
}

func TestNotifierStopsFeedingWatchdogWhenControlLoopIsStale(t *testing.T) {
	messages := make(chan string, 1)
	notifier := &Notifier{socket: "/test/notify.sock", watchdogTimeout: 300 * time.Millisecond,
		send: func(message string) error { messages <- message; return nil }}
	liveness := &Liveness{}
	liveness.last.Store(time.Now().Add(-time.Second).UnixNano())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notifier.Start(ctx, liveness, nil)
	select {
	case message := <-messages:
		t.Fatalf("控制循环卡死时不得继续喂 watchdog: %q", message)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestLivenessBoundedWorkLeasePreventsFalseStaleSignal(t *testing.T) {
	liveness := &Liveness{}
	liveness.last.Store(time.Now().Add(-time.Hour).UnixNano())
	if liveness.fresh(time.Second) {
		t.Fatal("无工作租约的过期控制循环不得视为存活")
	}
	release := liveness.Begin(time.Minute)
	if !liveness.fresh(time.Second) {
		t.Fatal("有界工作租约内应允许长操作继续")
	}
	release()
	liveness.last.Store(time.Now().Add(-time.Hour).UnixNano())
	if liveness.fresh(time.Second) {
		t.Fatal("工作租约释放后必须恢复进展新鲜度判定")
	}
}

func readNotification(t *testing.T, messages <-chan string, timeout time.Duration) string {
	t.Helper()
	select {
	case message := <-messages:
		return message
	case <-time.After(timeout):
		t.Fatal("未收到 sd_notify 消息")
		return ""
	}
}
