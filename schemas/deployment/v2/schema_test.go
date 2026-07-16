package deploymentv2

import "testing"

func TestParseClusterReplicas(t *testing.T) {
	deployment, err := Parse([]byte(`{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"api","kind":"service","plugins":[{"id":"com.example.api","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":3,"placement":{"nodeSelector":{"region":"cn"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if deployment.Units[0].Replicas != 3 || deployment.Units[0].Plugins[0].Channel != "stable" {
		t.Fatalf("集群规格未规范化: %+v", deployment.Units[0])
	}
}

func TestParseResourcesAffinityAndAutoscaling(t *testing.T) {
	raw := []byte(`{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"api","kind":"service","plugins":[{"id":"com.example.api","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":2,"autoscaling":{"min_replicas":2,"max_replicas":6,"metric":"queue.depth","target_value_per_replica":10},"resources":{"requests":{"cpu_millis":500,"memory_bytes":1073741824,"gpu":1}},"placement":{"nodeSelector":{"region":"cn"},"affinity":{"preferred":[{"match_labels":{"disk":"ssd"},"weight":80}]},"antiAffinity":{"required":[{"match_labels":{"maintenance":"true"}}]}}}]}`)
	deployment, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	unit := deployment.Units[0]
	if unit.Autoscaling == nil || unit.Autoscaling.MaxReplicas != 6 || unit.Resources.Requests.CPUMillis != 500 || len(unit.Placement.AntiAffinity.Required) != 1 {
		t.Fatalf("高级调度字段未完整解析: %+v", unit)
	}
}

func TestParseRejectsInvalidClusterDeployment(t *testing.T) {
	for name, raw := range map[string]string{
		"v1":                                  `{"version":1,"revision":1,"metadata":{"name":"prod"},"units":[]}`,
		"zero replicas":                       `{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"api","kind":"service","plugins":[{"id":"com.example.api","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":0}]}`,
		"autoscaling reversed bounds":         `{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"api","kind":"service","plugins":[{"id":"com.example.api","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":2,"autoscaling":{"min_replicas":4,"max_replicas":2,"metric":"queue","target_value_per_replica":10}}]}`,
		"replicas outside autoscaling bounds": `{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"api","kind":"service","plugins":[{"id":"com.example.api","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":1,"autoscaling":{"min_replicas":2,"max_replicas":4,"metric":"queue","target_value_per_replica":10}}]}`,
		"negative resources":                  `{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"api","kind":"service","plugins":[{"id":"com.example.api","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":1,"resources":{"requests":{"cpu_millis":-1}}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(raw)); err == nil {
				t.Fatal("非法集群部署必须 fail-closed")
			}
		})
	}
}
