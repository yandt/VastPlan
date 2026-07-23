package commonv1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateManagedCredentialRef(t *testing.T) {
	valid := ManagedCredentialRef{Handle: "credential://managed/opaque-1", Scope: "tenant", Owner: "cn.vastplan.demo", Purpose: "demo.token", Version: 1}
	if err := ValidateManagedCredentialRef(valid); err != nil {
		t.Fatal(err)
	}
	invalid := []ManagedCredentialRef{
		{},
		{Handle: "credential://named/demo", Scope: "tenant", Owner: "cn.vastplan.demo", Purpose: "demo.token", Version: 1},
		{Handle: valid.Handle, Scope: "user", Owner: valid.Owner, Purpose: valid.Purpose, Version: 1},
		{Handle: valid.Handle, Scope: "tenant", Owner: "INVALID", Purpose: valid.Purpose, Version: 1},
		{Handle: valid.Handle, Scope: "tenant", Owner: valid.Owner, Purpose: "token", Version: 1},
		{Handle: valid.Handle, Scope: "tenant", Owner: valid.Owner, Purpose: valid.Purpose, Version: 0},
	}
	for _, ref := range invalid {
		if err := ValidateManagedCredentialRef(ref); err == nil {
			t.Fatalf("非法 CredentialRef 必须拒绝: %+v", ref)
		}
	}
}

func TestManagedCredentialRefInteropVector(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "sdk-interop-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Ref ManagedCredentialRef `json:"managedCredentialRef"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil || ValidateManagedCredentialRef(vector.Ref) != nil {
		t.Fatalf("跨语言 CredentialRef 向量无效: %+v err=%v", vector.Ref, err)
	}
}
