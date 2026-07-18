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

func TestParseAppProfileReferencesWithoutServiceUnits(t *testing.T) {
	deployment, err := Parse([]byte(`{"version":2,"revision":1,"metadata":{"name":"runner-fleet"},"units":[],"app_profiles":[{"id":"collector","revision":3,"digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(deployment.Units) != 0 || len(deployment.AppProfiles) != 1 || deployment.AppProfiles[0].Revision != 3 {
		t.Fatalf("App Profile 引用未独立解析: %+v", deployment)
	}
}

func TestParseRejectsInvalidAppProfileReferences(t *testing.T) {
	for name, raw := range map[string]string{
		"invalid digest": `{"version":2,"revision":1,"metadata":{"name":"runner-fleet"},"units":[],"app_profiles":[{"id":"collector","revision":1,"digest":"not-a-digest"}]}`,
		"duplicate id":   `{"version":2,"revision":1,"metadata":{"name":"runner-fleet"},"units":[],"app_profiles":[{"id":"collector","revision":1,"digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"id":"collector","revision":2,"digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(raw)); err == nil {
				t.Fatal("非法 App Profile 引用必须 fail-closed")
			}
		})
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

func TestParseValidatesUnitDependencyDAG(t *testing.T) {
	valid := `{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"database","kind":"service","plugins":[{"id":"com.example.db","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":1},{"id":"api","kind":"service","plugins":[{"id":"com.example.api","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":1,"depends_on":["database"]}]}`
	if _, err := Parse([]byte(valid)); err != nil {
		t.Fatalf("有效依赖 DAG 不应拒绝: %v", err)
	}
	cycle := `{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"a","kind":"service","plugins":[{"id":"com.example.a","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":1,"depends_on":["b"]},{"id":"b","kind":"service","plugins":[{"id":"com.example.b","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":1,"depends_on":["a"]}]}`
	if _, err := Parse([]byte(cycle)); err == nil {
		t.Fatal("循环 unit 依赖必须拒绝")
	}
}

func TestParseValidatesLeaderAndPartitionOwnership(t *testing.T) {
	partitioned := `{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"db","kind":"service","plugins":[{"id":"com.example.db","version":"1.0.0"}],"enabled":true,"service_role":"backend","logical_service":"platform.database","instance_policy":"partitioned","state_model":"partition-owned","visibility":"cluster","routing":"shard","replicas":2,"partition_keys":["a","b","c"]}]}`
	deployment, err := Parse([]byte(partitioned))
	if err != nil || len(deployment.Units[0].PartitionKeys) != 3 {
		t.Fatalf("合法分片部署应通过: deployment=%+v err=%v", deployment, err)
	}
	invalid := []string{
		`{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"leader","kind":"service","plugins":[{"id":"com.example.leader","version":"1.0.0"}],"enabled":true,"service_role":"backend","instance_policy":"leader","state_model":"leader-owned","visibility":"cluster","routing":"leader","replicas":2}]}`,
		`{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"db","kind":"service","plugins":[{"id":"com.example.db","version":"1.0.0"}],"enabled":true,"service_role":"backend","instance_policy":"partitioned","state_model":"partition-owned","visibility":"cluster","routing":"shard","replicas":1}]}`,
	}
	for _, raw := range invalid {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatal("无 fencing/分片所有权边界的部署必须拒绝")
		}
	}
}
