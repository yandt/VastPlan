package platformcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
)

func TestFileProfileStoreCommitsWithCASAndOwnerPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bootstrap", "platform-control.json")
	store := &FileProfileStore{Path: path}
	profile := testProfile(path, 1)
	if err := store.Commit(context.Background(), profile, 0); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("Profile 权限错误: %v %v", info, err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded == nil || loaded.Generation != 1 {
		t.Fatalf("读取 Profile 失败: %+v %v", loaded, err)
	}
	if err := store.Commit(context.Background(), testProfile(path, 2), 0); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("陈旧 expected generation 必须冲突: %v", err)
	}
}

func TestFileProfileStoreRejectsBroadPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform-control.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&FileProfileStore{Path: path}).Load(context.Background()); err == nil {
		t.Fatal("权限过宽的 Profile 必须拒绝")
	}
}

func testProfile(secretPath string, generation uint64) platformcontrolv1.Profile {
	return platformcontrolv1.Profile{
		SchemaVersion: 1, Generation: generation,
		Connection: databasev1.ConnectionCandidate{
			ProviderID: "postgresql", Endpoint: "db.internal:5432", Database: "vastplan_control",
			Options: json.RawMessage(`{"user":"vastplan","tlsMode":"verify-full","serverName":"db.internal"}`), Pool: databasev1.DefaultPoolPolicy(),
		},
		Schema: "platform", SecretRef: platformcontrolv1.SecretRef{Kind: "owner-file", Path: secretPath + ".secret"}, ContractRange: "^1.0.0",
	}
}
