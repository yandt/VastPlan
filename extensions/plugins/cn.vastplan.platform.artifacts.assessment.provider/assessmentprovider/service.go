package assessmentprovider

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactsupplychain"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	provider "cdsoft.com.cn/VastPlan/extensions/sdk/go/artifactassessmentprovider"
	credentialmaterial "cdsoft.com.cn/VastPlan/extensions/sdk/go/credentialmaterial"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Service struct {
	config     Config
	provider   *provider.Provider
	downloader PackageDownloader
	now        func() time.Time
}

func New(config Config, scanner provider.Engine, downloader PackageDownloader) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if downloader == nil {
		return nil, errors.New("Assessment Provider downloader 不能为空")
	}
	runtime, err := provider.NewRuntime(provider.Config{ProviderID: config.ProviderID, KeyID: config.KeyID, TTL: config.TTL(), Maximum: config.Maximum}, scanner, config.WorkRoot)
	if err != nil {
		return nil, err
	}
	return &Service{config: config, provider: runtime, downloader: downloader, now: time.Now}, nil
}

func (s *Service) AssessAdmission(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, raw []byte) ([]byte, error) {
	if s == nil || host == nil || callCtx.GetTenantId() == "" || callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != artifactassessment.AssessmentControllerPluginID {
		return nil, errors.New("Assessment Provider 调用上下文无效")
	}
	var request artifactassessment.ProviderAssessmentRequest
	if err := decodeStrict(raw, &request); err != nil || artifactassessment.ValidateProviderAssessmentRequest(request) != nil {
		return nil, errors.New("Assessment Provider 请求无效")
	}
	lease, err := requestScanLease(ctx, host, callCtx.GetTenantId(), request.ScanLeaseRequest, s.now().UTC())
	if err != nil {
		return nil, err
	}
	packageBytes, err := s.downloader.Download(ctx, lease)
	if err != nil {
		return nil, err
	}
	packageDigest := sha256.Sum256(packageBytes)
	if hex.EncodeToString(packageDigest[:]) != request.SubjectSHA256 {
		return nil, errors.New("下载制品摘要与扫描租约不一致")
	}
	manifest, _, err := artifacttrust.InspectPackage(packageBytes)
	if err != nil || manifest.ID != request.Ref.PluginID || manifest.Version != request.Ref.Version || manifest.SupplyChain == nil || manifest.SupplyChain.SBOM == nil {
		return nil, errors.New("下载制品清单或 SBOM 绑定无效")
	}
	sbom, err := artifacttrust.ReadPackageFile(packageBytes, manifest.SupplyChain.SBOM.Path, artifactsupplychain.MaxCycloneDXBytes)
	if err != nil {
		return nil, err
	}
	sbomDigest := sha256.Sum256(sbom)
	if hex.EncodeToString(sbomDigest[:]) != request.SBOMSHA256 {
		return nil, errors.New("下载制品 SBOM 摘要与扫描租约不一致")
	}
	material, err := credentialmaterial.NewFromEnvironment(host, callCtx.GetTenantId(), s.config.SigningKeyRef)
	if err != nil {
		return nil, err
	}
	var evidence provider.Evidence
	err = material.WithMaterial(ctx, s.now().UTC(), func(secret credentialmaterial.Material) error {
		key, err := provider.ParseEd25519PrivateKey(secret.Bytes())
		if err != nil {
			return err
		}
		defer provider.ZeroPrivateKey(key)
		evidence, err = s.provider.AssessWithEvidenceKey(ctx, artifactassessment.ScanRequest{
			Identity: artifactassessment.ArtifactIdentity{PluginID: manifest.ID, Channel: request.Ref.Channel, Publisher: manifest.Publisher, SHA256: request.SubjectSHA256, SBOMSHA256: request.SBOMSHA256},
			Package:  packageBytes, SBOM: sbom, PolicyID: request.PolicyID,
		}, ed25519.PrivateKey(key))
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := archiveReport(s.config.ReportRoot, evidence.Admission.Evaluation.Vulnerabilities.ReportSHA256, evidence.Report); err != nil {
		return nil, err
	}
	return json.Marshal(evidence.Admission)
}

func requestScanLease(ctx context.Context, host sdk.Host, tenant string, request artifactassessment.ScanLeaseRequest, now time.Time) (artifactassessment.ScanLease, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return artifactassessment.ScanLease{}, err
	}
	operation := "prepareAssessment"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: "platform.artifacts.repository", Operation: &operation}, &contractv1.CallContext{TenantId: tenant}, payload)
	if err != nil || result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		return artifactassessment.ScanLease{}, errors.New("Repository 拒绝安全评估扫描租约")
	}
	var lease artifactassessment.ScanLease
	if err := decodeStrict(raw, &lease); err != nil || artifactassessment.ValidateScanLease(lease, PluginID, now) != nil {
		return artifactassessment.ScanLease{}, errors.New("Repository 返回无效安全评估扫描租约")
	}
	if lease.Ref != request.Ref || lease.SubjectSHA256 != request.SubjectSHA256 || lease.SBOMSHA256 != request.SBOMSHA256 {
		return artifactassessment.ScanLease{}, errors.New("Repository 扫描租约未绑定原请求")
	}
	return lease, nil
}

func decodeStrict(raw []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("JSON 只能包含一个文档")
	}
	return nil
}
