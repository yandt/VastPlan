//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
)

type faultNATSCluster struct {
	options []*natsserver.Options
	servers []*natsserver.Server
}

func newFaultNATSCluster(t *testing.T, count int) *faultNATSCluster {
	t.Helper()
	ports := reserveTCPPorts(t, count)
	routes := make([]*url.URL, count)
	for index, port := range ports {
		route, err := url.Parse(fmt.Sprintf("nats-route://127.0.0.1:%d", port))
		if err != nil {
			t.Fatal(err)
		}
		routes[index] = route
	}
	cluster := &faultNATSCluster{options: make([]*natsserver.Options, count), servers: make([]*natsserver.Server, count)}
	for index, port := range ports {
		cluster.options[index] = &natsserver.Options{
			ServerName: fmt.Sprintf("vastplan-a3-%d", index+1), Host: "127.0.0.1", Port: -1,
			JetStream: true, StoreDir: filepath.Join(t.TempDir(), "jetstream"),
			JetStreamMaxMemory: 256 << 20, JetStreamMaxStore: 512 << 20,
			Cluster: natsserver.ClusterOpts{Name: "vastplan-a3", Host: "127.0.0.1", Port: port},
			Routes:  append([]*url.URL(nil), routes...), NoLog: true, NoSigs: true,
		}
	}
	return cluster
}

func reserveTCPPorts(t *testing.T, count int) []int {
	t.Helper()
	listeners := make([]net.Listener, 0, count)
	for range count {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, listener)
	}
	ports := make([]int, count)
	for index, listener := range listeners {
		ports[index] = listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
	}
	return ports
}

func (c *faultNATSCluster) startAll(t *testing.T) {
	t.Helper()
	for index := range c.options {
		c.restart(t, index)
	}
	c.waitFormed(t)
}

func (c *faultNATSCluster) restart(t *testing.T, index int) {
	t.Helper()
	server, err := natsserver.NewServer(c.options[index])
	if err != nil {
		t.Fatalf("启动 NATS 节点 %d: %v", index, err)
	}
	c.servers[index] = server
	go server.Start()
	if !server.ReadyForConnections(5 * time.Second) {
		t.Fatalf("NATS 节点 %d 未就绪", index)
	}
}

func (c *faultNATSCluster) stop(index int) {
	if index < 0 || index >= len(c.servers) || c.servers[index] == nil {
		return
	}
	c.servers[index].Shutdown()
	c.servers[index].WaitForShutdown()
	c.servers[index] = nil
}

func (c *faultNATSCluster) shutdown() {
	for index := range c.servers {
		c.stop(index)
	}
}

func (c *faultNATSCluster) firstRunning() int {
	for index, server := range c.servers {
		if server != nil && server.Running() {
			return index
		}
	}
	return -1
}

func (c *faultNATSCluster) streamLeader(stream string) int {
	for index, server := range c.servers {
		if server != nil && server.JetStreamIsStreamLeader("$G", stream) {
			return index
		}
	}
	return -1
}

func (c *faultNATSCluster) waitFormed(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		formed, current, running, peerCount := true, true, 0, 0
		for _, server := range c.servers {
			if server == nil || !server.Running() {
				continue
			}
			running++
			formed = formed && server.NumRoutes() >= 2
			current = current && server.JetStreamIsCurrent()
			if server.JetStreamIsLeader() {
				peerCount = len(server.JetStreamClusterPeers())
			}
		}
		if running > 0 && formed && current && peerCount == running {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("三节点 NATS JetStream 未形成仲裁")
}

func (c *faultNATSCluster) waitForStreamReplicas(t *testing.T, stream string, replicas int) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		leaders, running := 0, 0
		for _, server := range c.servers {
			if server != nil && server.Running() {
				running++
				if server.JetStreamIsStreamLeader("$G", stream) {
					leaders++
				}
			}
		}
		if running == replicas && leaders == 1 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("stream %s 未形成 %d 副本且唯一 leader", stream, replicas)
}

func waitForCurrentStreamReplicas(t *testing.T, js jetstream.JetStream, streamName string, replicas int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		stream, err := js.Stream(ctx, streamName)
		if err == nil {
			info, infoErr := stream.Info(ctx)
			if infoErr == nil && info.Cluster != nil && info.Cluster.Leader != "" && len(info.Cluster.Replicas) == replicas-1 {
				current := true
				for _, replica := range info.Cluster.Replicas {
					current = current && replica.Current && !replica.Offline && replica.Lag == 0
				}
				if current {
					cancel()
					return
				}
				last = fmt.Sprintf("leader=%s replicas=%+v", info.Cluster.Leader, info.Cluster.Replicas)
			} else if infoErr != nil {
				last = infoErr.Error()
			}
		} else {
			last = err.Error()
		}
		cancel()
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("stream %s 副本未全部追平: %s", streamName, last)
}

func updateEventually(t *testing.T, store *sharedstate.NATSStore, scope sharedstate.Scope, key string, value []byte, revision uint64, timeout time.Duration) sharedstate.Entry {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		entry, err := store.Update(ctx, scope, key, value, revision)
		cancel()
		if err == nil {
			return entry
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("等待 Shared State 更新恢复超时: %v", lastErr)
	return sharedstate.Entry{}
}

func getEventually(t *testing.T, store *sharedstate.NATSStore, scope sharedstate.Scope, key string, timeout time.Duration) sharedstate.Entry {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		entry, err := store.Get(ctx, scope, key)
		cancel()
		if err == nil {
			return entry
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("等待 Shared State 读取恢复超时: %v", lastErr)
	return sharedstate.Entry{}
}
