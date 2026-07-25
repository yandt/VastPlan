package artifactapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type assessmentReportRepository struct{ raw []byte }

func (r assessmentReportRepository) Publish([]byte, []byte) (pluginv1.Artifact, error) {
	return pluginv1.Artifact{}, nil
}

func (r assessmentReportRepository) Read(pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, []byte, error) {
	return pluginv1.Artifact{}, nil, nil, nil
}

func (r assessmentReportRepository) ReadAssessmentReport(string) ([]byte, error) {
	return append([]byte(nil), r.raw...), nil
}

func TestAssessmentReportRequiresReadCredentialAndExactDigestPath(t *testing.T) {
	digest := strings.Repeat("a", 64)
	server := &Server{Repository: assessmentReportRepository{raw: []byte(`{"Results":[]}`)}, ReadToken: "reader"}

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/assessment-reports/"+digest, nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("报告不得匿名读取: %d", unauthorized.Code)
	}

	authorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/assessment-reports/"+digest, nil)
	request.Header.Set("Authorization", "Bearer reader")
	server.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK || authorized.Header().Get("Cache-Control") != "private, no-store" || authorized.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("报告响应安全头无效: code=%d headers=%v", authorized.Code, authorized.Header())
	}

	invalid := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/v1/assessment-reports/not-a-digest", nil)
	request.Header.Set("Authorization", "Bearer reader")
	server.ServeHTTP(invalid, request)
	if invalid.Code != http.StatusNotFound {
		t.Fatalf("非摘要路径必须隐藏为 404: %d", invalid.Code)
	}
}
