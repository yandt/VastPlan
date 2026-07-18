package deploymentcontroller

import (
	"fmt"
	"testing"

	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

func BenchmarkBackend_SchedulerEligibleNodes100(b *testing.B) {
	nodes := map[string]controlplane.NodeRecord{}
	available := map[string]controlplane.ResourceCapacity{}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("node-%03d", i)
		nodes[id] = controlplane.NodeRecord{NodeID: id, Labels: map[string]string{"zone": fmt.Sprintf("z%d", i%3)}}
		available[id] = controlplane.ResourceCapacity{CPUMillis: 8000, MemoryBytes: 16 << 30}
	}
	unit := deploymentv2.ServiceUnit{ID: "api", Resources: deploymentv2.ResourceRequirements{Requests: deploymentv2.ResourceList{CPUMillis: 500, MemoryBytes: 256 << 20}}, Placement: deploymentv2.Placement{NodeSelector: map[string]string{"zone": "z1"}}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(eligibleNodes(nodes, available, unit)) == 0 {
			b.Fatal("no nodes")
		}
	}
}
