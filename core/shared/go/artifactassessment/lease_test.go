package artifactassessment

import (
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestValidateScanLeaseRequiresExactAudienceAndSecretHTTPSURL(t *testing.T) {
	now := time.Date(2026, 7, 24, 5, 0, 0, 0, time.UTC)
	lease := ScanLease{
		SchemaVersion: SchemaVersion, Ref: pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.demo", Version: "1.2.3", Channel: "testing"},
		SubjectSHA256: strings.Repeat("a", 64), SBOMSHA256: strings.Repeat("b", 64), Audience: AssessmentProviderPluginID,
		URL: "https://repo.example/v1/artifacts/cn.vastplan.product.demo/1.2.3/testing/package?vp_ticket=" + strings.Repeat("c", 43), ExpiresAt: now.Add(ScanLeaseTTL),
	}
	if err := ValidateScanLease(lease, AssessmentProviderPluginID, now); err != nil {
		t.Fatal(err)
	}
	lease.Audience = "another.plugin"
	if err := ValidateScanLease(lease, AssessmentProviderPluginID, now); err == nil {
		t.Fatal("错误受众必须拒绝")
	}
	lease.Audience = AssessmentProviderPluginID
	lease.URL += "&debug=true"
	if err := ValidateScanLease(lease, AssessmentProviderPluginID, now); err == nil {
		t.Fatal("额外 query 必须拒绝，避免租约语义扩张")
	}
}
