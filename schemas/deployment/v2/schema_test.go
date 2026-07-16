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

func TestParseRejectsInvalidClusterDeployment(t *testing.T) {
	for name, raw := range map[string]string{
		"v1":            `{"version":1,"revision":1,"metadata":{"name":"prod"},"units":[]}`,
		"zero replicas": `{"version":2,"revision":1,"metadata":{"name":"prod"},"units":[{"id":"api","kind":"service","plugins":[{"id":"com.example.api","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":0}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(raw)); err == nil {
				t.Fatal("非法集群部署必须 fail-closed")
			}
		})
	}
}
