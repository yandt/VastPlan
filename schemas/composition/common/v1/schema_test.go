package compositioncommonv1

import (
	"encoding/json"
	"os"
	"testing"
)

type conformanceVectors struct {
	ValidKernels   []string `json:"valid_kernels"`
	InvalidKernels []string `json:"invalid_kernels"`
	ValidOrigins   []string `json:"valid_origins"`
	InvalidOrigins []string `json:"invalid_origins"`
}

func TestCommonTargetAndDigest(t *testing.T) {
	raw, err := os.ReadFile("testdata/conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors conformanceVectors
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, kernel := range vectors.ValidKernels {
		if err := ValidateTarget(Target{Kernel: kernel}, kernel); err != nil {
			t.Fatalf("合法目标 %q 被拒绝: %v", kernel, err)
		}
	}
	for _, kernel := range vectors.InvalidKernels {
		if err := ValidateKernel(kernel); err == nil {
			t.Fatalf("非法目标 %q 必须拒绝", kernel)
		}
	}
	for _, origin := range vectors.ValidOrigins {
		if err := ValidateOrigin(origin); err != nil {
			t.Fatalf("合法来源 %q 被拒绝: %v", origin, err)
		}
	}
	for _, origin := range vectors.InvalidOrigins {
		if err := ValidateOrigin(origin); err == nil {
			t.Fatalf("非法来源 %q 必须拒绝", origin)
		}
	}
	if err := ValidateTarget(Target{Kernel: KernelFrontend}, KernelBackend); err == nil {
		t.Fatal("跨内核输入必须拒绝")
	}
	document := Document{Version: 1, Revision: 2, ID: "sample"}
	if got := Digest(document); len(got) != 64 || got != Digest(document) {
		t.Fatalf("摘要必须稳定且为 SHA-256: %q", got)
	}
}
