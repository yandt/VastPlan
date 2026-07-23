package repositoryruntime

import (
	"path/filepath"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreport"
)

func TestAssessmentInventoryReportsArchiveReadinessWithoutPhysicalPath(t *testing.T) {
	archive, err := artifactreport.New(filepath.Join(t.TempDir(), "reports"))
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{assessmentReports: archive}
	status := manager.AssessmentInventory(time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC))
	if !status.ReportArchiveReady || !status.ObservedAt.Equal(time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)) || len(status.Revisions) != 0 {
		t.Fatalf("unexpected assessment inventory: %+v", status)
	}
}
