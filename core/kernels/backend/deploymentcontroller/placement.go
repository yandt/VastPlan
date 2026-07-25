package deploymentcontroller

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"sort"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
)

func eligibleNodes(nodes map[string]controlplane.NodeRecord, available map[string]controlplane.ResourceCapacity, unit deploymentv2.ServiceUnit) []string {
	var eligible []string
	for nodeID, node := range nodes {
		if matchesLabels(node.Labels, unit.Placement.NodeSelector) &&
			matchesRequiredAffinity(node.Labels, unit.Placement) &&
			fitsResources(available[nodeID], unit.Resources.Requests) {
			eligible = append(eligible, nodeID)
		}
	}
	return eligible
}

func matchesRequiredAffinity(labels map[string]string, placement deploymentv2.Placement) bool {
	for _, term := range placement.Affinity.Required {
		if !matchesLabels(labels, term.MatchLabels) {
			return false
		}
	}
	for _, term := range placement.AntiAffinity.Required {
		if matchesLabels(labels, term.MatchLabels) {
			return false
		}
	}
	return true
}

func preferenceScore(labels map[string]string, placement deploymentv2.Placement) int {
	score := 0
	for _, term := range placement.Affinity.Preferred {
		if matchesLabels(labels, term.MatchLabels) {
			score += term.Weight
		}
	}
	for _, term := range placement.AntiAffinity.Preferred {
		if matchesLabels(labels, term.MatchLabels) {
			score -= term.Weight
		}
	}
	return score
}

func matchesLabels(labels, selector map[string]string) bool {
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func fitsResources(capacity controlplane.ResourceCapacity, request deploymentv2.ResourceList) bool {
	return capacity.CPUMillis >= request.CPUMillis && capacity.MemoryBytes >= request.MemoryBytes && capacity.GPU >= request.GPU
}

func placementScore(unitID, nodeID string) uint64 {
	digest := sha256.Sum256([]byte(unitID + "\x00" + nodeID))
	return binary.BigEndian.Uint64(digest[:8])
}

// assignPartitions 为每个分片选择稳定 owner。先用 rendezvous 为每个候选节点保留
// 一个分片，再为剩余分片选择最高分节点，既避免空副本，也尽量减少节点变化时的迁移。
func assignPartitions(unit deploymentv2.ServiceUnit, nodes []string) map[string][]string {
	assigned := make(map[string][]string, len(nodes))
	if unit.InstancePolicy != servicemodel.PolicyPartitioned {
		for _, nodeID := range nodes {
			assigned[nodeID] = nil
		}
		return assigned
	}
	keys := append([]string(nil), unit.PartitionKeys...)
	sort.Strings(keys)
	remaining := append([]string(nil), keys...)
	for _, nodeID := range nodes {
		best := 0
		for index := 1; index < len(remaining); index++ {
			if placementScore(unit.ID+"\x00"+remaining[index], nodeID) > placementScore(unit.ID+"\x00"+remaining[best], nodeID) {
				best = index
			}
		}
		assigned[nodeID] = append(assigned[nodeID], remaining[best])
		remaining = append(remaining[:best], remaining[best+1:]...)
	}
	for _, key := range remaining {
		owner := nodes[0]
		for _, nodeID := range nodes[1:] {
			if placementScore(unit.ID+"\x00"+key, nodeID) > placementScore(unit.ID+"\x00"+key, owner) {
				owner = nodeID
			}
		}
		assigned[owner] = append(assigned[owner], key)
	}
	for nodeID := range assigned {
		sort.Strings(assigned[nodeID])
	}
	return assigned
}

func cloneConfig(config map[string]any) map[string]any {
	if config == nil {
		return nil
	}
	raw, _ := json.Marshal(config)
	var clone map[string]any
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func assignmentsEqual(planned map[string]deploymentv1.DesiredState, existing map[string]existingAssignment) bool {
	if len(planned) != len(existing) {
		return false
	}
	byNode := make(map[string]deploymentv1.DesiredState, len(existing))
	for _, item := range existing {
		byNode[item.NodeID] = item.State
	}
	for nodeID, state := range planned {
		old, ok := byNode[nodeID]
		if !ok {
			return false
		}
		state.Revision, old.Revision = 0, 0
		if state.Digest() != old.Digest() {
			return false
		}
	}
	return true
}
