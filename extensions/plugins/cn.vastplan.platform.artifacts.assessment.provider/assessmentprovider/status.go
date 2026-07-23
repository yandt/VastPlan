package assessmentprovider

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	provider "cdsoft.com.cn/VastPlan/extensions/sdk/go/artifactassessmentprovider"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) AssessStatus(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, raw []byte) ([]byte, error) {
	if s == nil || host == nil || callCtx.GetTenantId() == "" || callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != artifactassessment.AssessmentControllerPluginID {
		return nil, errors.New("Assessment Provider 复扫调用上下文无效")
	}
	var request artifactassessment.ProviderStatusRequest
	if err := decodeStrict(raw, &request); err != nil || artifactassessment.ValidateProviderStatusRequest(request) != nil {
		return nil, errors.New("Assessment Provider 复扫请求无效")
	}
	scan, err := s.prepareScan(ctx, host, callCtx.GetTenantId(), request.ProviderAssessmentRequest)
	if err != nil {
		return nil, err
	}
	var evidence provider.StatusEvidence
	err = s.withSigningKey(ctx, host, callCtx.GetTenantId(), func(key ed25519.PrivateKey) error {
		evidence, err = s.provider.AssessStatusWithEvidenceKey(ctx, provider.StatusRequest{Scan: scan, AdmissionSHA256: request.AdmissionSHA256, Sequence: request.Sequence, PreviousSHA256: request.PreviousSHA256}, key)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := archiveReport(s.config.ReportRoot, evidence.Status.Evaluation.Vulnerabilities.ReportSHA256, evidence.Report); err != nil {
		return nil, err
	}
	return json.Marshal(evidence.Status)
}
