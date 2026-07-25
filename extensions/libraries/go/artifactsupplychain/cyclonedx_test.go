package artifactsupplychain

import "testing"

func TestInspectCycloneDXReturnsBoundedSummary(t *testing.T) {
	raw := []byte(`{"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"serialNumber":"urn:uuid:test","metadata":{"component":{"type":"application","name":"cn.vastplan.demo","version":"1.0.0"}},"components":[{"type":"library","name":"example","version":"2.0.0"}]}`)
	summary, err := InspectCycloneDX(raw)
	if err != nil || summary.RootName != "cn.vastplan.demo" || summary.RootVersion != "1.0.0" || summary.Components != 1 || len(summary.SHA256) != 64 {
		t.Fatalf("CycloneDX 摘要无效: summary=%+v err=%v", summary, err)
	}
}

func TestInspectCycloneDXRejectsWrongFormatAndMissingSubject(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"bomFormat":"SPDX","specVersion":"1.5","version":1,"metadata":{"component":{"name":"demo","version":"1"}}}`),
		[]byte(`{"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"metadata":{"component":{}}}`),
	} {
		if _, err := InspectCycloneDX(raw); err == nil {
			t.Fatalf("非法 SBOM 必须拒绝: %s", raw)
		}
	}
}
