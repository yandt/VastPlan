package artifactassessmentprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

type TrivyConfig struct {
	Binary           string
	CacheDirectory   string
	ScannerVersion   string
	DatabaseRevision string
	Timeout          time.Duration
	AllowedLicenses  []string
	FullLicenseScan  bool
}

type Trivy struct{ config TrivyConfig }

func NewTrivy(config TrivyConfig) (*Trivy, error) {
	if !filepath.IsAbs(config.Binary) || filepath.Clean(config.Binary) != config.Binary ||
		!filepath.IsAbs(config.CacheDirectory) || filepath.Clean(config.CacheDirectory) != config.CacheDirectory {
		return nil, errors.New("Trivy binary 与 cacheDirectory 必须是规范绝对路径")
	}
	if strings.TrimSpace(config.ScannerVersion) != config.ScannerVersion || config.ScannerVersion == "" || len(config.ScannerVersion) > 80 ||
		!validDigest(config.DatabaseRevision) || config.Timeout < time.Second || config.Timeout > 2*time.Hour {
		return nil, errors.New("Trivy 版本、数据库 revision 或超时无效")
	}
	allowed := append([]string(nil), config.AllowedLicenses...)
	for index, value := range allowed {
		if value == "" || strings.TrimSpace(value) != value || slices.Contains(allowed[:index], value) {
			return nil, errors.New("Trivy 许可证白名单必须规范且不重复")
		}
	}
	config.AllowedLicenses = allowed
	return &Trivy{config: config}, nil
}

func (t *Trivy) Scan(ctx context.Context, workspace string) (EngineResult, error) {
	if t == nil || ctx == nil || !filepath.IsAbs(workspace) {
		return EngineResult{}, errors.New("Trivy 扫描请求无效")
	}
	ctx, cancel := context.WithTimeout(ctx, t.config.Timeout)
	defer cancel()
	if err := t.verifyBinary(ctx); err != nil {
		return EngineResult{}, err
	}
	databaseRevision, err := databaseSnapshotDigest(t.config.CacheDirectory)
	if err != nil {
		return EngineResult{}, err
	}
	if databaseRevision != t.config.DatabaseRevision {
		return EngineResult{}, errors.New("Trivy 数据库快照摘要与配置 revision 不一致")
	}
	reportPath := filepath.Join(filepath.Dir(workspace), "trivy-report.json")
	args := []string{"filesystem", "--quiet", "--format", "json", "--output", reportPath,
		"--scanners", "vuln,license", "--list-all-pkgs", "--offline-scan", "--skip-db-update", "--cache-dir", t.config.CacheDirectory}
	if t.config.FullLicenseScan {
		args = append(args, "--license-full")
	}
	args = append(args, workspace)
	stderr := &limitedBuffer{limit: 1 << 20}
	command := exec.CommandContext(ctx, t.config.Binary, args...)
	command.Stdout = io.Discard
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return EngineResult{}, fmt.Errorf("Trivy 扫描超时: %w", ctx.Err())
		}
		return EngineResult{}, fmt.Errorf("Trivy 扫描失败: %w: %s", err, stderr.String())
	}
	report, err := readBoundedFile(reportPath, MaxReportBytes)
	if err != nil {
		return EngineResult{}, err
	}
	result, err := t.normalize(report)
	if err != nil {
		return EngineResult{}, err
	}
	result.Scanner = artifactassessment.Scanner{ID: DefaultScannerID, Version: t.config.ScannerVersion, DatabaseRevision: t.config.DatabaseRevision}
	result.Report = report
	return result, nil
}

type trivyReport struct {
	SchemaVersion int `json:"SchemaVersion"`
	Results       []struct {
		Target          string            `json:"Target"`
		Packages        []json.RawMessage `json:"Packages"`
		Vulnerabilities []struct {
			ID       string `json:"VulnerabilityID"`
			Package  string `json:"PkgName"`
			Version  string `json:"InstalledVersion"`
			Severity string `json:"Severity"`
		} `json:"Vulnerabilities"`
		Licenses []struct {
			Name     string `json:"Name"`
			Package  string `json:"PkgName"`
			Severity string `json:"Severity"`
		} `json:"Licenses"`
	} `json:"Results"`
}

func (t *Trivy) normalize(raw []byte) (EngineResult, error) {
	var report trivyReport
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&report); err != nil {
		return EngineResult{}, fmt.Errorf("解析 Trivy JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return EngineResult{}, errors.New("Trivy 报告必须只包含一个 JSON 文档")
	}
	if report.SchemaVersion != 2 || report.Results == nil {
		return EngineResult{}, errors.New("Trivy JSON schemaVersion 或 results 无效")
	}
	result := EngineResult{}
	packages := 0
	vulnerabilities := map[string]struct{}{}
	licenses := map[string]struct{}{}
	for _, item := range report.Results {
		packages += len(item.Packages)
		for _, finding := range item.Vulnerabilities {
			key := strings.Join([]string{item.Target, finding.ID, finding.Package, finding.Version}, "\x00")
			if finding.ID == "" || strings.TrimSpace(finding.ID) != finding.ID {
				return EngineResult{}, errors.New("Trivy 漏洞发现缺少规范 ID")
			}
			if _, exists := vulnerabilities[key]; exists {
				continue
			}
			vulnerabilities[key] = struct{}{}
			result.Vulnerabilities = append(result.Vulnerabilities, VulnerabilityFinding{
				ID: finding.ID, Package: finding.Package, Version: finding.Version, Target: item.Target, Severity: normalizeSeverity(finding.Severity),
			})
		}
		for _, finding := range item.Licenses {
			name := strings.TrimSpace(finding.Name)
			if name == "" {
				name = "UNKNOWN"
			}
			key := strings.Join([]string{item.Target, name, finding.Package}, "\x00")
			if _, exists := licenses[key]; exists {
				continue
			}
			licenses[key] = struct{}{}
			disposition := LicenseDenied
			if strings.EqualFold(name, "UNKNOWN") || strings.EqualFold(finding.Severity, "UNKNOWN") {
				disposition = LicenseUnknown
			} else if slices.Contains(t.config.AllowedLicenses, name) {
				disposition = LicenseAllowed
			}
			result.Licenses = append(result.Licenses, LicenseFinding{Name: name, Package: finding.Package, Target: item.Target, Disposition: disposition})
		}
	}
	if packages == 0 && len(result.Licenses) == 0 {
		return EngineResult{}, errors.New("Trivy 没有识别到任何包或许可证")
	}
	return result, nil
}

func (t *Trivy) verifyBinary(ctx context.Context) error {
	info, err := os.Lstat(t.config.Binary)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("Trivy binary 不存在、不是普通文件或是符号链接")
	}
	output := &limitedBuffer{limit: 64 << 10}
	command := exec.CommandContext(ctx, t.config.Binary, "--version")
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		return fmt.Errorf("读取 Trivy 版本: %w", err)
	}
	if !strings.Contains(output.String(), "Version: "+t.config.ScannerVersion) {
		return fmt.Errorf("Trivy 实际版本与配置 %s 不一致", t.config.ScannerVersion)
	}
	return nil
}

func normalizeSeverity(value string) Severity {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "CRITICAL":
		return SeverityCritical
	case "HIGH":
		return SeverityHigh
	case "MEDIUM":
		return SeverityMedium
	case "LOW":
		return SeverityLow
	default:
		return SeverityUnknown
	}
}

func readBoundedFile(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > max {
		return nil, errors.New("Trivy 报告不存在、不是普通文件或大小超限")
	}
	return os.ReadFile(path)
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		return len(value), nil
	}
	if len(value) > remaining {
		_, _ = b.Buffer.Write(value[:remaining])
		return len(value), nil
	}
	return b.Buffer.Write(value)
}
