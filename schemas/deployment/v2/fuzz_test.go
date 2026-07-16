package deploymentv2

import (
	"encoding/json"
	"testing"
)

func FuzzParseDeploymentNeverPanics(f *testing.F) {
	f.Add([]byte(`{"version":2,"revision":1,"metadata":{"name":"demo","tenant":"acme"},"units":[]}`))
	f.Add([]byte(`null`))
	f.Add([]byte{0xff, 0x01})
	f.Fuzz(func(t *testing.T, raw []byte) {
		deployment, err := Parse(raw)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(deployment)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(encoded); err != nil {
			t.Fatalf("有效部署重编码后不再满足 Schema: %v", err)
		}
	})
}
