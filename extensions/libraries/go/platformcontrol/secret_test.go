package platformcontrol

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
)

func TestOwnerFileSecretRequiresOwnerOnlyAndClearsMaterial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := ResolveSecretSource(platformcontrolv1.SecretRef{Kind: "owner-file", Path: path}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := source.WithSecret(context.Background(), func(value []byte) error {
		if string(value) != "secret" {
			t.Fatalf("secret = %q", value)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := source.WithSecret(context.Background(), func([]byte) error { return nil }); err == nil {
		t.Fatal("group-readable owner file must be rejected")
	}
}
