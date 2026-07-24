// Command testpublish signs and uploads one already-built first-party test
// artifact to the managed repository advertised by the local development
// platform. It intentionally cannot target production or arbitrary servers.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const maximumPackageBytes = int64(256 << 20)

var developmentPrerelease = regexp.MustCompile(`^dev\.[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)+$`)

type options struct {
	PackageFile     string
	StateRoot       string
	StatusURL       string
	BackendTarget   string
	BackendBinding  string
	FrontendTarget  string
	FrontendBinding string
	FrontendScope   string
	Timeout         time.Duration
}

type developmentStatus struct {
	Ready                bool   `json:"ready"`
	Mode                 string `json:"mode"`
	ProductionEquivalent bool   `json:"productionEquivalent"`
	RunDir               string `json:"runDir"`
	Portal               string `json:"portal"`
	Repositories         struct {
		Testing struct {
			Protocol      string `json:"protocol"`
			Endpoint      string `json:"endpoint"`
			ProfileDigest string `json:"profileDigest"`
			Persistent    bool   `json:"persistent"`
			Ready         bool   `json:"ready"`
		} `json:"testing"`
	} `json:"repositories"`
}

func main() {
	var opts options
	flag.StringVar(&opts.PackageFile, "package", "", "已构建且清单使用 dev.* 预发布版本的插件 .tar.gz")
	flag.StringVar(&opts.StateRoot, "state-root", ".vastplan/dev-platform", "本地平台开发状态根")
	flag.StringVar(&opts.StatusURL, "status-url", "http://127.0.0.1:18080/__vastplan_dev/status", "本地平台状态端点（仅允许回环地址）")
	flag.StringVar(&opts.BackendTarget, "backend-target", "", "可选：发布到 Backend 测试目标，格式 deployment/unit")
	flag.StringVar(&opts.BackendBinding, "backend-binding", "", "可选：复用已有 TestTargetBinding；与 -backend-target 同用时作为绑定 ID")
	flag.StringVar(&opts.FrontendTarget, "frontend-target", "", "可选：发布到 Frontend Application 测试目标，值为 portal ID")
	flag.StringVar(&opts.FrontendBinding, "frontend-binding", "", "可选：复用已有 Frontend TestTargetBinding；与 -frontend-target 同用时作为绑定 ID")
	flag.StringVar(&opts.FrontendScope, "frontend-scope", string(portalapi.TestTargetApplicationPlugin), "Frontend 目标范围：application-plugin 或 platform-profile-plugin")
	flag.DurationVar(&opts.Timeout, "timeout", 2*time.Minute, "上传超时")
	flag.Parse()
	if err := publish(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "测试制品发布失败: %v\n", err)
		os.Exit(1)
	}
}

func publish(ctx context.Context, opts options) error {
	if strings.TrimSpace(opts.PackageFile) == "" {
		return errors.New("必须提供 -package")
	}
	if opts.Timeout <= 0 || opts.Timeout > 10*time.Minute {
		return errors.New("-timeout 必须在 0 到 10 分钟之间")
	}
	stateRoot, err := filepath.Abs(opts.StateRoot)
	if err != nil {
		return err
	}
	stateRoot, err = filepath.EvalSymlinks(stateRoot)
	if err != nil {
		return fmt.Errorf("解析开发状态根: %w", err)
	}
	statusEndpoint, err := loopbackURL(opts.StatusURL, false)
	if err != nil {
		return fmt.Errorf("状态端点: %w", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	status, err := readStatus(ctx, client, statusEndpoint.String())
	if err != nil {
		return err
	}
	if !status.Ready || status.Mode != "local-development" || status.ProductionEquivalent {
		return errors.New("目标不是已就绪的本地开发平台")
	}
	if !status.Repositories.Testing.Persistent || !status.Repositories.Testing.Ready {
		return errors.New("本地平台测试仓库尚未就绪；请先发布包含仓库服务的平台基线")
	}
	runDir, err := confinedRunDir(stateRoot, status.RunDir)
	if err != nil {
		return err
	}
	repositoryProfile, err := artifactrepositoryv1.ParseProfileFile(filepath.Join(runDir, "repository-profile.json"))
	if err != nil {
		return err
	}
	if repositoryProfile.Protocol != status.Repositories.Testing.Protocol || repositoryProfile.Endpoint != status.Repositories.Testing.Endpoint || repositoryProfile.Digest() != status.Repositories.Testing.ProfileDigest {
		return errors.New("状态端点与受管 Repository Profile 身份不一致")
	}

	packageBytes, manifest, artifact, err := loadTestingArtifact(opts.PackageFile)
	if err != nil {
		return err
	}
	if manifest.Publisher != "vastplan" || !strings.HasPrefix(manifest.ID, "cn.vastplan.") {
		return errors.New("本地测试发布器当前只允许 VastPlan 第一方插件")
	}
	trustFile := filepath.Join(stateRoot, "repositories", "testing", "artifact-trust.json")
	if err := requireRegularFile(trustFile, false); err != nil {
		return err
	}
	trust, err := pluginservice.LoadTrustStore(trustFile)
	if err != nil {
		return err
	}
	if repositoryProfile.Protocol == artifactrepositoryv1.ProtocolLocalTest {
		artifact, receipt, wasExisting, err := publishLocalTest(ctx, runDir, stateRoot, repositoryProfile, trust, manifest, artifact, packageBytes)
		if err != nil {
			return err
		}
		printReceipt(artifact, repositoryProfile.Endpoint, receipt.Revision, wasExisting)
		return submitRequestedTestReleases(ctx, status, opts, receipt)
	}
	if repositoryProfile.Protocol != artifactrepositoryv1.ProtocolRemote {
		return errors.New("本地发布器没有精确匹配 Repository Profile 的 Adapter")
	}
	repositoryURL, err := loopbackURL(repositoryProfile.Endpoint, true)
	if err != nil {
		return fmt.Errorf("remote-compat 测试仓库地址: %w", err)
	}
	publishToken, err := readSecret(filepath.Join(runDir, "secrets", "artifact-publish.token"))
	if err != nil {
		return err
	}
	readToken, err := readSecret(filepath.Join(runDir, "secrets", "artifact-read.token"))
	if err != nil {
		return err
	}
	repositoryClient, err := tlsClient(filepath.Join(runDir, "secrets", "tls-cert.pem"), opts.Timeout)
	if err != nil {
		return err
	}
	revision, found, err := lookupRepositoryRevision(ctx, repositoryClient, repositoryURL, readToken, artifact)
	if err != nil {
		return fmt.Errorf("查询既有 Catalog: %w", err)
	}
	wasExisting := found
	if !found {
		reader := &pluginservice.RemoteRepository{BaseURL: repositoryURL.String(), Token: readToken, Trust: trust, Client: repositoryClient}
		existing, fetchErr := reader.Fetch(ctx, pluginservice.Ref{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel})
		var attestation pluginservice.Attestation
		if fetchErr == nil {
			if existing.Artifact.SHA256 != artifact.SHA256 || existing.Artifact.Size != artifact.Size {
				return errors.New("测试仓库已经存在相同 ref 但摘要不同的不可变制品")
			}
			if err := json.Unmarshal(existing.Proof, &attestation); err != nil {
				return fmt.Errorf("解析仓库既有证明: %w", err)
			}
		} else if !errors.Is(fetchErr, artifacttrust.ErrNotFound) {
			return fmt.Errorf("检查仓库既有制品: %w", fetchErr)
		} else {
			privateKeyFile := filepath.Join(stateRoot, "repositories", "testing", "secrets", "artifact-signing.pem")
			if err := requireRegularFile(privateKeyFile, true); err != nil {
				return err
			}
			privateKey, err := pluginservice.LoadEd25519PrivateKeyPEM(privateKeyFile)
			if err != nil {
				return err
			}
			attestation, err = pluginservice.SignArtifact(artifact, manifest.Publisher, "local-testing", privateKey, time.Now().UTC())
			if err != nil {
				return err
			}
		}
		remote := &pluginservice.RemoteRepository{
			BaseURL: repositoryURL.String(), Token: publishToken, Trust: trust, Client: repositoryClient,
		}
		published, err := remote.PublishRemote(ctx, attestation, packageBytes)
		if err != nil {
			return err
		}
		artifact = published
		revision, found, err = lookupRepositoryRevision(ctx, repositoryClient, repositoryURL, readToken, published)
		if err != nil {
			return fmt.Errorf("确认 Catalog 与发布流水账: %w", err)
		}
		if !found {
			return errors.New("Catalog 未返回刚发布的精确制品")
		}
	}
	receipt := artifactrepositoryv1.Receipt{SchemaVersion: artifactrepositoryv1.ProfileVersion, RepositoryID: repositoryProfile.ID, Protocol: repositoryProfile.Protocol, ProfileDigest: repositoryProfile.Digest(), Ref: pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}, SHA256: artifact.SHA256, Revision: revision}
	if err := artifactrepositoryv1.ValidateReceipt(repositoryProfile, receipt); err != nil {
		return err
	}
	printReceipt(artifact, repositoryURL.String(), revision, wasExisting)
	return submitRequestedTestReleases(ctx, status, opts, receipt)
}

func submitRequestedTestReleases(ctx context.Context, status developmentStatus, opts options, receipt artifactrepositoryv1.Receipt) error {
	if opts.BackendTarget != "" || opts.BackendBinding != "" {
		if err := submitBackendTestRelease(ctx, status, opts, receipt); err != nil {
			return err
		}
	}
	if opts.FrontendTarget != "" || opts.FrontendBinding != "" {
		if err := submitFrontendTestRelease(ctx, status, opts, receipt); err != nil {
			return err
		}
	}
	return nil
}

func lookupRepositoryRevision(ctx context.Context, client *http.Client, repositoryURL *url.URL, readToken string, artifact pluginservice.Artifact) (uint64, bool, error) {
	query := url.Values{
		"pluginId": {artifact.PluginID}, "version": {artifact.Version}, "channel": {artifact.Channel},
		"page": {"1"}, "pageSize": {"1"},
	}
	endpoint := strings.TrimRight(repositoryURL.String(), "/") + "/v1/catalog/artifacts?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, false, err
	}
	request.Header.Set("Authorization", "Bearer "+readToken)
	response, err := client.Do(request)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return 0, false, fmt.Errorf("Catalog 返回 %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	var page struct {
		Total int `json:"total"`
		Items []struct {
			Ref                pluginv1.ArtifactRef `json:"ref"`
			SHA256             string               `json:"sha256"`
			Publisher          string               `json:"publisher"`
			KeyID              string               `json:"keyId"`
			RepositoryRevision uint64               `json:"repositoryRevision"`
		} `json:"items"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&page); err != nil {
		return 0, false, err
	}
	if page.Total == 0 && len(page.Items) == 0 {
		return 0, false, nil
	}
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	manifest, manifestErr := pluginv1.ParseManifest(artifact.Manifest)
	if manifestErr != nil {
		return 0, false, manifestErr
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Ref != ref || page.Items[0].SHA256 != artifact.SHA256 ||
		page.Items[0].Publisher != manifest.Publisher || page.Items[0].KeyID != "local-testing" || page.Items[0].RepositoryRevision == 0 {
		return 0, false, errors.New("Catalog 中的精确引用与本地制品不一致")
	}
	return page.Items[0].RepositoryRevision, true, nil
}

func printReceipt(artifact pluginservice.Artifact, repositoryEndpoint string, revision uint64, existing bool) {
	status := "已发布测试制品"
	if existing {
		status = "测试制品已存在，按原 revision 幂等返回"
	}
	fmt.Printf("%s %s@%s/testing\nrepository: %s\nrevision: %d\nsha256: %s\n",
		status, artifact.PluginID, artifact.Version, repositoryEndpoint, revision, artifact.SHA256)
}

func readStatus(ctx context.Context, client *http.Client, endpoint string) (developmentStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return developmentStatus{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return developmentStatus{}, fmt.Errorf("读取本地平台状态: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return developmentStatus{}, fmt.Errorf("本地平台状态返回 %s", response.Status)
	}
	var status developmentStatus
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&status); err != nil {
		return developmentStatus{}, fmt.Errorf("解析本地平台状态: %w", err)
	}
	return status, nil
}

func loadTestingArtifact(filename string) ([]byte, pluginv1.Manifest, pluginservice.Artifact, error) {
	if err := requireRegularFile(filename, false); err != nil {
		return nil, pluginv1.Manifest{}, pluginservice.Artifact{}, err
	}
	info, err := os.Stat(filename)
	if err != nil {
		return nil, pluginv1.Manifest{}, pluginservice.Artifact{}, err
	}
	if info.Size() <= 0 || info.Size() > maximumPackageBytes {
		return nil, pluginv1.Manifest{}, pluginservice.Artifact{}, fmt.Errorf("制品大小必须在 1 到 %d 字节之间", maximumPackageBytes)
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, pluginv1.Manifest{}, pluginservice.Artifact{}, err
	}
	artifact, err := pluginservice.Describe("testing", raw)
	if err != nil {
		return nil, pluginv1.Manifest{}, pluginservice.Artifact{}, err
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return nil, pluginv1.Manifest{}, pluginservice.Artifact{}, err
	}
	version, err := semver.StrictNewVersion(manifest.Version)
	if err != nil || !developmentPrerelease.MatchString(version.Prerelease()) {
		return nil, pluginv1.Manifest{}, pluginservice.Artifact{}, errors.New("测试制品版本必须是唯一的 dev.* SemVer 预发布版本，例如 0.4.0-dev.20260720.3.a81c33f")
	}
	return raw, manifest, artifact, nil
}

func loopbackURL(raw string, requireHTTPS bool) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("必须是无用户信息的绝对 URL")
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return nil, errors.New("必须使用 HTTPS")
	}
	if !requireHTTPS && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("只允许 HTTP 或 HTTPS")
	}
	if requireHTTPS && (parsed.RawQuery != "" || (parsed.Path != "" && parsed.Path != "/")) {
		return nil, errors.New("测试仓库 URL 不能包含路径或查询参数")
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "localhost") {
		address := net.ParseIP(host)
		if address == nil || !address.IsLoopback() {
			return nil, errors.New("测试发布器只允许 localhost 或回环 IP")
		}
	}
	return parsed, nil
}

func confinedRunDir(stateRoot, rawRunDir string) (string, error) {
	runDir, err := filepath.Abs(rawRunDir)
	if err != nil {
		return "", err
	}
	runDir, err = filepath.EvalSymlinks(runDir)
	if err != nil {
		return "", fmt.Errorf("解析活动运行目录: %w", err)
	}
	info, err := os.Stat(runDir)
	if err != nil || !info.IsDir() {
		return "", errors.New("状态端点返回的活动运行目录无效")
	}
	relative, err := filepath.Rel(stateRoot, runDir)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("状态端点返回的运行目录不属于配置的开发状态根")
	}
	if first := strings.Split(relative, string(filepath.Separator))[0]; first != "runs" {
		return "", errors.New("状态端点返回的不是受管 runs 目录")
	}
	return runDir, nil
}

func requireRegularFile(filename string, ownerOnly bool) error {
	info, err := os.Lstat(filename)
	if err != nil {
		return fmt.Errorf("读取文件属性 %s: %w", filename, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s 必须是普通文件且不能是符号链接", filename)
	}
	if ownerOnly && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s 权限过宽 %o", filename, info.Mode().Perm())
	}
	return nil
}

func readSecret(filename string) (string, error) {
	if err := requireRegularFile(filename, true); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	secret := strings.TrimSpace(string(raw))
	if secret == "" {
		return "", fmt.Errorf("秘密文件为空: %s", filename)
	}
	return secret, nil
}

func tlsClient(certificateFile string, timeout time.Duration) (*http.Client, error) {
	if err := requireRegularFile(certificateFile, false); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(certificateFile)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		return nil, errors.New("本地平台 CA 文件不包含有效证书")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}
