package configurationscopedv1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	configurationscopedv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationscoped/v1"
)

func TestScopedConfigurationWireIsClosedAndValueDigestIsCanonical(t *testing.T) {
	parsed, err := configurationscopedv1.ParseRequest(configurationscopedv1.OperationResolve, []byte(`{}`))
	if err != nil || parsed == nil {
		t.Fatalf("解析 resolve: %+v err=%v", parsed, err)
	}
	if _, err := configurationscopedv1.ParseRequest(configurationscopedv1.OperationResolve, []byte(`{"tenant":"forged"}`)); err == nil {
		t.Fatal("请求不得携带自报 tenant")
	}
	first, err := configurationscopedv1.DigestValues(json.RawMessage(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	second, _ := configurationscopedv1.DigestValues(json.RawMessage(`{ "a": 1, "b": 2 }`))
	if first != second || len(first) != 64 {
		t.Fatalf("canonical digest 不稳定: %s %s", first, second)
	}
}

func TestResolutionDistinguishesSignedSeedFromActive(t *testing.T) {
	id := "cfg_" + strings.Repeat("a", 24)
	values := json.RawMessage(`{"greetingTemplate":"Hello, {{name}}"}`)
	digest, _ := configurationscopedv1.DigestValues(values)
	response := configurationscopedv1.Resolution{
		Protocol: configurationscopedv1.Protocol, ConfigurationID: id, Scope: configurationscopedv1.ScopeTenant,
		Revision: 0, Digest: digest, SchemaDigest: strings.Repeat("b", 64), ArtifactSHA256: strings.Repeat("c", 64),
		Values: values, Source: "seed", ObservedAt: time.Now().UTC(),
	}
	if err := configurationscopedv1.ValidateResolution(response); err != nil {
		t.Fatal(err)
	}
	response.Source = "active"
	if err := configurationscopedv1.ValidateResolution(response); err == nil {
		t.Fatal("revision 0 不得伪装 Active")
	}
}

func TestWatchCarriesOnlyRevisionFacts(t *testing.T) {
	response := configurationscopedv1.RevisionObservation{
		Protocol: configurationscopedv1.Protocol, ConfigurationID: "cfg_" + strings.Repeat("a", 24), Changed: true,
		Revision: 2, Digest: strings.Repeat("d", 64), ObservedAt: time.Now().UTC(),
	}
	if err := configurationscopedv1.ValidateRevisionObservation(response); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(response)
	if strings.Contains(string(raw), "values") || strings.Contains(string(raw), "subject") || strings.Contains(string(raw), "tenant") {
		t.Fatalf("watch 不得泄漏值或 scope 身份: %s", raw)
	}
}
