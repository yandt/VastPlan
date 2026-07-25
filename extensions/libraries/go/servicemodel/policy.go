// Package servicemodel 定义插件服务实例策略、状态模型、能力可见性和路由的单一真源。
package servicemodel

import "fmt"

const (
	PolicyPerKernel    = "per-kernel"
	PolicyActiveActive = "active-active"
	PolicyLeader       = "leader"
	PolicyPartitioned  = "partitioned"

	StateLocalEphemeral = "local-ephemeral"
	StateExternalShared = "external-shared"
	StateLeaderOwned    = "leader-owned"
	StatePartitionOwned = "partition-owned"

	VisibilityLocal   = "local"
	VisibilityService = "service"
	VisibilityCluster = "cluster"
	VisibilityGlobal  = "global"

	RoutingDirect = "direct"
	RoutingQueue  = "queue"
	RoutingLeader = "leader"
	RoutingShard  = "shard"
)

// Policy 是一组已解析的插件服务运行策略。空字段由 Normalize 按实例策略补齐。
type Policy struct {
	InstancePolicy string `json:"instancePolicy,omitempty"`
	StateModel     string `json:"stateModel,omitempty"`
	Visibility     string `json:"visibility,omitempty"`
	Routing        string `json:"routing,omitempty"`
	RoutingDomain  string `json:"routingDomain,omitempty"`
}

// Normalize 补齐策略默认值。无 runtime 的旧清单按历史 mesh 行为解释为 active-active。
func Normalize(policy Policy) Policy {
	if policy.InstancePolicy == "" {
		policy.InstancePolicy = PolicyActiveActive
	}
	if policy.StateModel == "" {
		switch policy.InstancePolicy {
		case PolicyPerKernel:
			policy.StateModel = StateLocalEphemeral
		case PolicyLeader:
			policy.StateModel = StateLeaderOwned
		case PolicyPartitioned:
			policy.StateModel = StatePartitionOwned
		default:
			policy.StateModel = StateExternalShared
		}
	}
	if policy.Visibility == "" {
		if policy.InstancePolicy == PolicyPerKernel {
			policy.Visibility = VisibilityLocal
		} else {
			policy.Visibility = VisibilityCluster
		}
	}
	if policy.Routing == "" {
		switch policy.InstancePolicy {
		case PolicyPerKernel:
			policy.Routing = RoutingDirect
		case PolicyLeader:
			policy.Routing = RoutingLeader
		case PolicyPartitioned:
			policy.Routing = RoutingShard
		default:
			policy.Routing = RoutingQueue
		}
	}
	return policy
}

// Validate 检查策略组合是否能由当前运行时语义正确执行。
func Validate(raw Policy) error {
	p := Normalize(raw)
	switch p.InstancePolicy {
	case PolicyPerKernel, PolicyActiveActive, PolicyLeader, PolicyPartitioned:
	default:
		return fmt.Errorf("未知 instance policy %q", p.InstancePolicy)
	}
	switch p.StateModel {
	case StateLocalEphemeral, StateExternalShared, StateLeaderOwned, StatePartitionOwned:
	default:
		return fmt.Errorf("未知 state model %q", p.StateModel)
	}
	switch p.Visibility {
	case VisibilityLocal, VisibilityService, VisibilityCluster, VisibilityGlobal:
	default:
		return fmt.Errorf("未知 capability visibility %q", p.Visibility)
	}
	switch p.Routing {
	case RoutingDirect, RoutingQueue, RoutingLeader, RoutingShard:
	default:
		return fmt.Errorf("未知 capability routing %q", p.Routing)
	}
	switch p.InstancePolicy {
	case PolicyPerKernel:
		if p.StateModel != StateLocalEphemeral || p.Visibility != VisibilityLocal || p.Routing != RoutingDirect {
			return fmt.Errorf("per-kernel 必须使用 local-ephemeral + local + direct")
		}
	case PolicyActiveActive:
		if p.StateModel != StateExternalShared || p.Visibility == VisibilityLocal || p.Routing != RoutingQueue {
			return fmt.Errorf("active-active 必须使用 external-shared、非 local visibility 和 queue")
		}
	case PolicyLeader:
		if (p.StateModel != StateLeaderOwned && p.StateModel != StateExternalShared) || p.Visibility == VisibilityLocal || p.Routing != RoutingLeader {
			return fmt.Errorf("leader 必须使用 leader-owned 或 external-shared、非 local visibility 和 leader")
		}
	case PolicyPartitioned:
		if p.StateModel != StatePartitionOwned || p.Visibility == VisibilityLocal || p.Routing != RoutingShard {
			return fmt.Errorf("partitioned 必须使用 partition-owned、非 local visibility 和 shard")
		}
	}
	return nil
}

// Equal 判断两个策略在规范化后是否完全一致。
func Equal(left, right Policy) bool {
	return Normalize(left) == Normalize(right)
}

// QueueIdentity 返回 active-active/leader/partitioned 数据面使用的稳定 queue 身份。
// 空逻辑服务和路由域保留 v1 capability-only 行为，避免旧实例与新实例互相摘流。
func QueueIdentity(logicalService, capability, routingDomain string) string {
	if logicalService == "" && routingDomain == "" {
		return capability
	}
	return logicalService + "\x00" + capability + "\x00" + routingDomain
}
