package main

import (
	"errors"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

// buildSharedStateDependency is the sole composition-root decision between the
// bootstrap NATS store and the durable Platform Control binding.
func buildSharedStateDependency(plane *nodeControlPlane, platformControl *platformControlCoordinator) (sharedstate.Store, error) {
	if platformControl != nil {
		if platformControl.binding == nil {
			return nil, errors.New("Platform Control SQL 模式缺少 Shared State Binding")
		}
		return platformControl.binding, nil
	}
	if plane == nil || plane.buckets.SharedState == nil {
		return nil, nil
	}
	return sharedstate.NewNATSStore(plane.buckets.SharedState)
}
