// Package servicewatchdog implements the narrow sd_notify protocol used by the
// production Linux service manager. It has no effect when NOTIFY_SOCKET is not
// present, so the same kernel binary remains usable in local development.
package servicewatchdog

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const maxNotifyMessageBytes = 64 << 10

type Notifier struct {
	socket          string
	watchdogTimeout time.Duration
	send            func(string) error
}

// FromEnvironment reads the systemd notification contract. WATCHDOG_PID for a
// different process disables only watchdog pings; READY/STOPPING remain valid.
func FromEnvironment() (*Notifier, error) {
	socket := strings.TrimSpace(os.Getenv("NOTIFY_SOCKET"))
	if socket == "" {
		return &Notifier{}, nil
	}
	if len(socket) > 4096 || socket[0] != '@' && !filepath.IsAbs(socket) {
		return nil, errors.New("NOTIFY_SOCKET 必须是绝对或 abstract Unix socket")
	}
	notifier := &Notifier{socket: socket}
	if rawPID := strings.TrimSpace(os.Getenv("WATCHDOG_PID")); rawPID != "" {
		pid, err := strconv.Atoi(rawPID)
		if err != nil || pid <= 0 {
			return nil, errors.New("WATCHDOG_PID 无效")
		}
		if pid != os.Getpid() {
			return notifier, nil
		}
	}
	rawTimeout := strings.TrimSpace(os.Getenv("WATCHDOG_USEC"))
	if rawTimeout == "" {
		return notifier, nil
	}
	microseconds, err := strconv.ParseInt(rawTimeout, 10, 64)
	if err != nil || microseconds <= 0 || microseconds > int64((24*time.Hour)/time.Microsecond) {
		return nil, errors.New("WATCHDOG_USEC 无效")
	}
	notifier.watchdogTimeout = time.Duration(microseconds) * time.Microsecond
	return notifier, nil
}

func (n *Notifier) Enabled() bool { return n != nil && n.socket != "" }

func (n *Notifier) WatchdogTimeout() time.Duration {
	if n == nil {
		return 0
	}
	return n.watchdogTimeout
}

func (n *Notifier) Ready(status string) error {
	return n.notify("READY=1", statusField(status))
}

func (n *Notifier) Stopping(status string) error {
	return n.notify("STOPPING=1", statusField(status))
}

func (n *Notifier) watchdog() error { return n.notify("WATCHDOG=1") }

func statusField(status string) string {
	status = strings.TrimSpace(strings.ReplaceAll(status, "\n", " "))
	if status == "" {
		return ""
	}
	if len(status) > 1024 {
		status = status[:1024]
	}
	return "STATUS=" + status
}

func (n *Notifier) notify(fields ...string) error {
	if !n.Enabled() {
		return nil
	}
	filtered := fields[:0]
	for _, field := range fields {
		if field != "" {
			filtered = append(filtered, field)
		}
	}
	payload := strings.Join(filtered, "\n")
	if payload == "" || len(payload) > maxNotifyMessageBytes {
		return errors.New("sd_notify 消息为空或超限")
	}
	if n.send != nil {
		return n.send(payload)
	}
	path := n.socket
	if path[0] == '@' {
		path = "\x00" + path[1:]
	}
	connection, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		return fmt.Errorf("连接 sd_notify socket: %w", err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte(payload)); err != nil {
		return fmt.Errorf("发送 sd_notify: %w", err)
	}
	return nil
}

// Liveness is advanced by the Node Agent control loop. A separate watchdog
// goroutine may send keep-alives only while this timestamp remains fresh,
// preventing a healthy scheduler goroutine from masking a stuck reconciler.
type Liveness struct {
	last       atomic.Int64
	leaseUntil atomic.Int64
}

func (l *Liveness) Pulse() {
	if l != nil {
		l.last.Store(time.Now().UnixNano())
	}
}

// Begin grants one bounded work lease for an operation that may legitimately
// spend longer than half a systemd watchdog interval inside a blocking driver.
// The caller must also apply the same deadline to that operation. A hung driver
// can therefore delay watchdog recovery only until the explicit lease expires.
func (l *Liveness) Begin(maxDuration time.Duration) func() {
	if l == nil || maxDuration <= 0 {
		return func() {}
	}
	l.Pulse()
	until := time.Now().Add(maxDuration).UnixNano()
	l.leaseUntil.Store(until)
	return func() { l.leaseUntil.CompareAndSwap(until, 0) }
}

func (l *Liveness) age() time.Duration {
	if l == nil || l.last.Load() == 0 {
		return 1<<63 - 1
	}
	return time.Since(time.Unix(0, l.last.Load()))
}

func (l *Liveness) fresh(maxAge time.Duration) bool {
	if l == nil {
		return false
	}
	if until := l.leaseUntil.Load(); until > time.Now().UnixNano() {
		return true
	}
	return l.age() <= maxAge
}

// Start sends WATCHDOG=1 at one third of the negotiated timeout while the
// Agent event loop is making progress. It returns immediately when watchdog is
// disabled. Notification errors are logged and left for systemd to adjudicate.
func (n *Notifier) Start(ctx context.Context, liveness *Liveness, logf func(string, ...any)) {
	if n == nil || n.watchdogTimeout <= 0 {
		return
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	interval := n.watchdogTimeout / 3
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !liveness.fresh(n.watchdogTimeout / 2) {
					logf("Node Agent 控制循环未进展，停止 systemd watchdog 通知")
					continue
				}
				if err := n.watchdog(); err != nil {
					logf("systemd watchdog 通知失败: %v", err)
				}
			}
		}
	}()
}
