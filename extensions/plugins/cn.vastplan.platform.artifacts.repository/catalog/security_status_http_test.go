package catalog

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

type securityStatusHTTPService struct {
	ref pluginv1.ArtifactRef
	raw []byte
}

func (s *securityStatusHTTPService) Query(Query) Page                { return Page{} }
func (s *securityStatusHTTPService) Journal(uint64, int) JournalPage { return JournalPage{} }
func (s *securityStatusHTTPService) Resolve(pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactLock, error) {
	return pluginv1.ArtifactLock{}, nil
}
func (s *securityStatusHTTPService) AppendSecurityStatus(ref pluginv1.ArtifactRef, raw []byte, _ time.Time) (*artifactassessment.StatusRecord, string, error) {
	s.ref, s.raw = ref, append([]byte(nil), raw...)
	return &artifactassessment.StatusRecord{Sequence: 1, Evaluation: artifactassessment.Evaluation{Decision: artifactassessment.DecisionFail}}, "digest", nil
}
func (s *securityStatusHTTPService) ReadSecurityStatusChain(ref pluginv1.ArtifactRef) ([]byte, error) {
	s.ref = ref
	return []byte(`{"records":[{}]}`), nil
}

func TestSecurityStatusHTTPSeparatesScannerAndReadTokens(t *testing.T) {
	service := &securityStatusHTTPService{}
	handler := &HTTPHandler{Store: service, ReadToken: "reader", AssessmentToken: "scanner", RequireTLS: true}
	body := []byte(`{"ref":{"pluginId":"cn.example.plugin","version":"1.0.0","channel":"stable"},"record":{"schemaVersion":"v1"}}`)

	post := func(token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "https://repo.example/v1/catalog/security-status", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := post("reader"); response.Code != http.StatusUnauthorized {
		t.Fatalf("read token must not publish security status: %d", response.Code)
	}
	if response := post("scanner"); response.Code != http.StatusCreated || len(service.raw) == 0 {
		t.Fatalf("scanner token failed to publish status: code=%d body=%s", response.Code, response.Body.String())
	}

	get := func(token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "https://repo.example/v1/catalog/security-status?pluginId=cn.example.plugin&version=1.0.0&channel=stable", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := get("scanner"); response.Code != http.StatusUnauthorized {
		t.Fatalf("scanner token must not read status chain: %d", response.Code)
	}
	if response := get("reader"); response.Code != http.StatusOK {
		t.Fatalf("read token failed to read status chain: %d", response.Code)
	}
}
