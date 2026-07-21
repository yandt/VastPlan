package frontendcompositionv1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

func TestNodeManagementBindingDigestMatchesGoCanonicalJSON(t *testing.T) {
	var fixture struct {
		Binding PortalBinding `json:"binding"`
		Digest  string        `json:"digest"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "management-binding-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePortalBinding(fixture.Binding); err != nil {
		t.Fatalf("共享 Management Binding fixture 无效: %v", err)
	}
	if got := compositioncommonv1.Digest(fixture.Binding); got != fixture.Digest {
		t.Fatalf("Go Management Binding digest=%s, Node golden=%s", got, fixture.Digest)
	}
}
