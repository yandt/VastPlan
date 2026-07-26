package addressing

import (
	"testing"
	"time"
)

func TestInstancesExcludeExpiredLeases(t *testing.T) {
	router := &Router{instances: map[string]map[string]Announcement{
		"platform.database": {
			"expired": {Capability: "platform.database", Health: "healthy", Readiness: "ready", LeaseExpiresAt: time.Now().UTC().Add(-time.Second)},
			"live":    {Capability: "platform.database", Health: "healthy", Readiness: "ready", LeaseExpiresAt: time.Now().UTC().Add(time.Minute)},
		},
	}}
	instances := router.Instances("platform.database")
	if len(instances) != 1 || !instances[0].LeaseExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("过期租约不得继续提供 capability: %+v", instances)
	}
}

func TestTopologyChangeSubscriptionBroadcasts(t *testing.T) {
	router := &Router{topologySubscribers: map[uint64]chan struct{}{}}
	updates, cancel := router.SubscribeTopologyChanges()
	defer cancel()
	router.mu.Lock()
	router.notifyTopologyChangeLocked()
	router.mu.Unlock()
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("能力拓扑变化必须立即广播")
	}
}
