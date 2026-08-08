package platformcontrol

import "sync"

// RuntimeInstance is a verified capability-directory projection. The trusted
// invoker returns local-node instances first, followed by stable instance ID.
type RuntimeInstance struct {
	ID     string
	NodeID string
}

// runtimeReplicaSet is shared by every RemoteStore generation created from one
// Bootstrapper. Topology reconciliation can add or remove opened Runtime
// instances without replacing the kernel's stable Shared State Store SPI.
type runtimeReplicaSet struct {
	mu     sync.RWMutex
	opened map[string]struct{}
	change chan struct{}
}

func newRuntimeReplicaSet() *runtimeReplicaSet {
	return &runtimeReplicaSet{opened: map[string]struct{}{}, change: make(chan struct{})}
}

func (s *runtimeReplicaSet) Replace(instanceIDs []string) {
	next := make(map[string]struct{}, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		if instanceID != "" {
			next[instanceID] = struct{}{}
		}
	}
	s.mu.Lock()
	s.opened = next
	close(s.change)
	s.change = make(chan struct{})
	s.mu.Unlock()
}

func (s *runtimeReplicaSet) Ready(instanceIDs []string) (bool, <-chan struct{}) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, instanceID := range instanceIDs {
		if _, opened := s.opened[instanceID]; !opened {
			return false, s.change
		}
	}
	return true, s.change
}

func (s *runtimeReplicaSet) Preferred(available []RuntimeInstance) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]string, 0, len(available))
	for _, instance := range available {
		if _, opened := s.opened[instance.ID]; opened {
			result = append(result, instance.ID)
		}
	}
	return result
}
