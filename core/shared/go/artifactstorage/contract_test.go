package artifactstorage_test

import (
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactstorage"
)

func TestIdentifiers(t *testing.T) {
	if err := artifactstorage.ValidateProviderID("platform.artifacts.storage.file"); err != nil {
		t.Fatal(err)
	}
	if err := artifactstorage.ValidateVolumeID("repository.primary"); err != nil {
		t.Fatal(err)
	}
	if err := artifactstorage.ValidateMigrationID("repository.migration-001"); err != nil {
		t.Fatal(err)
	}
	if err := artifactstorage.ValidateMigrationPhase(artifactstorage.MigrationSync); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "../escape", "UpperCase", "a/b"} {
		if artifactstorage.ValidateVolumeID(value) == nil {
			t.Fatalf("非法 volume id 必须拒绝: %q", value)
		}
	}
	if artifactstorage.ValidateMigrationID("../escape") == nil || artifactstorage.ValidateMigrationPhase("delete") == nil {
		t.Fatal("非法迁移标识与阶段必须拒绝")
	}
}
