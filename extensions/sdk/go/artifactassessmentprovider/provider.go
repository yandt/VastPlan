package artifactassessmentprovider

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactsupplychain"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

type Provider struct {
	config     Config
	privateKey ed25519.PrivateKey
	engine     Engine
	now        func() time.Time
	workRoot   string
}

func New(config Config, privateKey ed25519.PrivateKey, engine Engine, workRoot string) (*Provider, error) {
	if strings.TrimSpace(config.ProviderID) != config.ProviderID || config.ProviderID == "" ||
		strings.TrimSpace(config.KeyID) != config.KeyID || config.KeyID == "" || len(config.ProviderID) > 160 || len(config.KeyID) > 160 {
		return nil, errors.New("安全评估 Provider 身份无效")
	}
	if config.TTL <= 0 || config.TTL > 365*24*time.Hour {
		return nil, errors.New("安全评估 Provider TTL 必须在 0..365 天内")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("安全评估 Provider 私钥不是 Ed25519")
	}
	if engine == nil {
		return nil, errors.New("安全评估扫描引擎不能为空")
	}
	if !filepath.IsAbs(workRoot) || filepath.Clean(workRoot) != workRoot {
		return nil, errors.New("安全评估工作根目录必须是规范绝对路径")
	}
	return &Provider{config: config, privateKey: append(ed25519.PrivateKey(nil), privateKey...), engine: engine, now: time.Now, workRoot: workRoot}, nil
}

func (p *Provider) Assess(ctx context.Context, request artifactassessment.ScanRequest) (artifactassessment.AdmissionRecord, error) {
	evidence, err := p.AssessWithEvidence(ctx, request)
	return evidence.Admission, err
}

func (p *Provider) AssessWithEvidence(ctx context.Context, request artifactassessment.ScanRequest) (Evidence, error) {
	if p == nil || ctx == nil {
		return Evidence{}, errors.New("安全评估 Provider 未初始化")
	}
	if err := validateRequest(request); err != nil {
		return Evidence{}, err
	}
	if err := os.MkdirAll(p.workRoot, 0o700); err != nil {
		return Evidence{}, fmt.Errorf("创建安全评估工作根目录: %w", err)
	}
	if err := secureDirectory(p.workRoot); err != nil {
		return Evidence{}, err
	}
	workspace, err := os.MkdirTemp(p.workRoot, "assessment-")
	if err != nil {
		return Evidence{}, fmt.Errorf("创建安全评估临时目录: %w", err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	packageRoot := filepath.Join(workspace, "package")
	if err := os.Mkdir(packageRoot, 0o700); err != nil {
		return Evidence{}, fmt.Errorf("创建安全评估制品目录: %w", err)
	}
	if err := extractPackage(request.Package, packageRoot); err != nil {
		return Evidence{}, err
	}
	result, err := p.engine.Scan(ctx, packageRoot)
	if err != nil {
		return Evidence{}, fmt.Errorf("执行安全评估扫描: %w", err)
	}
	if err := validateEngineResult(result); err != nil {
		return Evidence{}, err
	}
	evaluation := normalizeEvaluation(request, result, p.config, p.now().UTC())
	record, err := artifactassessment.SignAdmission(artifactassessment.AdmissionRecord{
		Evaluation: evaluation, ProviderID: p.config.ProviderID, KeyID: p.config.KeyID, PolicyID: request.PolicyID,
	}, p.privateKey)
	if err != nil {
		return Evidence{}, err
	}
	return Evidence{Admission: record, Report: append([]byte(nil), result.Report...)}, nil
}

func validateRequest(request artifactassessment.ScanRequest) error {
	identity := request.Identity
	if identity.PluginID == "" || identity.Channel == "" || identity.Publisher == "" || request.PolicyID == "" || len(request.Package) == 0 || len(request.SBOM) == 0 {
		return errors.New("安全评估请求身份、策略、制品或 SBOM 不完整")
	}
	packageDigest := sha256.Sum256(request.Package)
	sbomDigest := sha256.Sum256(request.SBOM)
	if hex.EncodeToString(packageDigest[:]) != identity.SHA256 || hex.EncodeToString(sbomDigest[:]) != identity.SBOMSHA256 {
		return errors.New("安全评估请求未绑定实际制品或 SBOM 摘要")
	}
	manifest, _, err := artifacttrust.InspectPackage(request.Package)
	if err != nil {
		return fmt.Errorf("安全评估制品无效: %w", err)
	}
	if manifest.ID != identity.PluginID || manifest.Publisher != identity.Publisher {
		return errors.New("安全评估制品清单与请求身份不一致")
	}
	if manifest.SupplyChain == nil || manifest.SupplyChain.SBOM == nil || manifest.SupplyChain.SBOM.SHA256 != identity.SBOMSHA256 {
		return errors.New("安全评估制品没有绑定请求中的 CycloneDX SBOM")
	}
	embedded, err := artifacttrust.ReadPackageFile(request.Package, manifest.SupplyChain.SBOM.Path, artifactsupplychain.MaxCycloneDXBytes)
	if err != nil || !bytes.Equal(embedded, request.SBOM) {
		return errors.New("安全评估请求 SBOM 与制品内绑定文件不一致")
	}
	return nil
}

func validateEngineResult(result EngineResult) error {
	if result.Scanner.ID == "" || result.Scanner.Version == "" || result.Scanner.DatabaseRevision == "" {
		return errors.New("扫描引擎没有返回完整身份")
	}
	if len(result.Report) == 0 || int64(len(result.Report)) > MaxReportBytes {
		return errors.New("扫描引擎报告为空或超限")
	}
	for _, item := range result.Vulnerabilities {
		if item.ID == "" || item.Severity != SeverityCritical && item.Severity != SeverityHigh && item.Severity != SeverityMedium && item.Severity != SeverityLow && item.Severity != SeverityUnknown {
			return errors.New("扫描引擎返回无效漏洞发现")
		}
	}
	for _, item := range result.Licenses {
		if item.Name == "" || item.Disposition != LicenseAllowed && item.Disposition != LicenseDenied && item.Disposition != LicenseUnknown {
			return errors.New("扫描引擎返回无效许可证发现")
		}
	}
	return nil
}

func normalizeEvaluation(request artifactassessment.ScanRequest, result EngineResult, config Config, evaluatedAt time.Time) artifactassessment.Evaluation {
	reportDigest := sha256.Sum256(result.Report)
	digest := hex.EncodeToString(reportDigest[:])
	vulnerabilities := artifactassessment.VulnerabilitySummary{ReportSHA256: digest}
	for _, finding := range result.Vulnerabilities {
		switch finding.Severity {
		case SeverityCritical:
			vulnerabilities.Critical++
		case SeverityHigh:
			vulnerabilities.High++
		case SeverityMedium:
			vulnerabilities.Medium++
		case SeverityLow:
			vulnerabilities.Low++
		default:
			vulnerabilities.Unknown++
		}
	}
	licenses := artifactassessment.LicenseSummary{ReportSHA256: digest}
	for _, finding := range result.Licenses {
		switch finding.Disposition {
		case LicenseAllowed:
			licenses.Allowed++
		case LicenseDenied:
			licenses.Denied++
		default:
			licenses.Unknown++
		}
	}
	evaluation := artifactassessment.Evaluation{
		SubjectSHA256: request.Identity.SHA256, SBOMSHA256: request.Identity.SBOMSHA256,
		Scanner: result.Scanner, Vulnerabilities: vulnerabilities, Licenses: licenses,
		Decision: artifactassessment.DecisionPass, EvaluatedAt: evaluatedAt, ExpiresAt: evaluatedAt.Add(config.TTL),
	}
	if exceedsMaximum(config.Maximum, evaluation) {
		evaluation.Decision = artifactassessment.DecisionFail
	}
	return evaluation
}

func exceedsMaximum(max artifactassessment.MaximumFindings, value artifactassessment.Evaluation) bool {
	checks := []struct {
		limit  *uint64
		actual uint64
	}{
		{max.Critical, value.Vulnerabilities.Critical}, {max.High, value.Vulnerabilities.High},
		{max.Medium, value.Vulnerabilities.Medium}, {max.Low, value.Vulnerabilities.Low},
		{max.UnknownVulnerability, value.Vulnerabilities.Unknown}, {max.DeniedLicense, value.Licenses.Denied},
		{max.UnknownLicense, value.Licenses.Unknown},
	}
	for _, check := range checks {
		if check.limit != nil && check.actual > *check.limit {
			return true
		}
	}
	return false
}

func secureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("安全评估工作目录必须是仅属主可访问且非符号链接的目录")
	}
	return nil
}
