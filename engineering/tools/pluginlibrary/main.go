// Command pluginlibrary installs exact remote artifacts into the Local Plugin
// Library advertised by the running development platform.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository/locallibrary"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository/localtest"
)

type options struct {
	StateRoot, StatusURL                        string
	RemoteProfile, RemoteTokenFile, RemoteTrust string
	RemoteCA, PluginID, Version, Channel        string
	Target, KernelVersion, Platform             string
	AllowedPublishers, AllowedPrefixes          string
	Timeout                                     time.Duration
}

type developmentStatus struct {
	Ready                bool   `json:"ready"`
	Mode                 string `json:"mode"`
	ProductionEquivalent bool   `json:"productionEquivalent"`
	RunDir               string `json:"runDir"`
	Repositories         struct {
		Testing struct {
			Protocol, Endpoint, ProfileDigest string
			Persistent, Ready                 bool
		} `json:"testing"`
	} `json:"repositories"`
}

func main() {
	var opts options
	flag.StringVar(&opts.StateRoot, "state-root", ".vastplan/dev-platform", "本地平台开发状态根")
	flag.StringVar(&opts.StatusURL, "status-url", "http://127.0.0.1:18080/__vastplan_dev/status", "本地平台状态端点")
	flag.StringVar(&opts.RemoteProfile, "remote-profile", "", "artifact.repository.remote.v1 Profile JSON")
	flag.StringVar(&opts.RemoteTokenFile, "remote-token-file", "", "远端仓库只读令牌文件")
	flag.StringVar(&opts.RemoteTrust, "remote-trust", "", "远端仓库发布者信任快照")
	flag.StringVar(&opts.RemoteCA, "remote-ca", "", "可选远端仓库私有 CA PEM")
	flag.StringVar(&opts.PluginID, "plugin", "", "精确插件 ID")
	flag.StringVar(&opts.Version, "version", "", "精确 SemVer")
	flag.StringVar(&opts.Channel, "channel", "stable", "精确 channel")
	flag.StringVar(&opts.Target, "target", "", "目标内核：backend、frontend、desktop 或 mobile")
	flag.StringVar(&opts.KernelVersion, "kernel-version", "0.1.0", "目标内核版本")
	flag.StringVar(&opts.Platform, "platform", "", "可选目标平台，例如 linux/amd64")
	flag.StringVar(&opts.AllowedPublishers, "allowed-publishers", "vastplan", "逗号分隔的允许发布者")
	flag.StringVar(&opts.AllowedPrefixes, "allowed-prefixes", "", "可选逗号分隔的允许插件 ID 前缀")
	flag.DurationVar(&opts.Timeout, "timeout", 2*time.Minute, "单次同步超时")
	flag.Parse()
	if err := install(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "本地插件库安装失败: %v\n", err)
		os.Exit(1)
	}
}

func install(parent context.Context, opts options) error {
	if opts.PluginID == "" || opts.Version == "" || opts.Channel == "" || opts.Target == "" || opts.KernelVersion == "" || opts.RemoteProfile == "" || opts.RemoteTokenFile == "" || opts.RemoteTrust == "" {
		return errors.New("必须提供 remote-profile、remote-token-file、remote-trust、plugin、version、channel、target 和 kernel-version")
	}
	if opts.Timeout <= 0 || opts.Timeout > 10*time.Minute {
		return errors.New("timeout 必须在 0 到 10 分钟之间")
	}
	ctx, cancel := context.WithTimeout(parent, opts.Timeout)
	defer cancel()
	stateRoot, err := filepath.Abs(opts.StateRoot)
	if err != nil {
		return err
	}
	statusURL, err := url.Parse(opts.StatusURL)
	if err != nil || statusURL.Scheme != "http" || statusURL.Hostname() != "127.0.0.1" && statusURL.Hostname() != "localhost" {
		return errors.New("开发状态端点必须是本机回环 HTTP URL")
	}
	status, err := readStatus(ctx, statusURL.String())
	if err != nil {
		return err
	}
	if !status.Ready || status.Mode != "local-development" || status.ProductionEquivalent || !status.Repositories.Testing.Persistent || !status.Repositories.Testing.Ready {
		return errors.New("本地开发平台或 Local Plugin Library 尚未就绪")
	}
	runDir, err := confinedRunDir(stateRoot, status.RunDir)
	if err != nil {
		return err
	}
	localProfile, err := artifactrepositoryv1.ParseProfileFile(filepath.Join(runDir, "repository-profile.json"))
	if err != nil {
		return err
	}
	if localProfile.Protocol != artifactrepositoryv1.ProtocolLocalTest || localProfile.Protocol != status.Repositories.Testing.Protocol || localProfile.Endpoint != status.Repositories.Testing.Endpoint || localProfile.Digest() != status.Repositories.Testing.ProfileDigest {
		return errors.New("运行状态与 Local Plugin Library Profile 不一致")
	}
	localToken, err := readSecret(filepath.Join(runDir, "secrets", "artifact-local-test.token"))
	if err != nil {
		return err
	}
	destination, err := localtest.NewClient(localProfile, localToken)
	if err != nil {
		return err
	}
	defer destination.CloseIdleConnections()

	remoteProfile, err := artifactrepositoryv1.ParseProfileFile(opts.RemoteProfile)
	if err != nil {
		return err
	}
	if remoteProfile.Protocol != artifactrepositoryv1.ProtocolRemote {
		return errors.New("remote-profile 必须使用 artifact.repository.remote.v1")
	}
	remoteToken, err := readSecret(opts.RemoteTokenFile)
	if err != nil {
		return err
	}
	trust, err := artifactrepository.LoadTrustStore(opts.RemoteTrust)
	if err != nil {
		return err
	}
	httpClient, err := remoteHTTPClient(opts.RemoteCA, opts.Timeout)
	if err != nil {
		return err
	}
	source, err := artifactrepository.NewRemoteAdapter(remoteProfile, &artifactrepository.RemoteRepository{
		BaseURL: remoteProfile.Endpoint, Token: remoteToken, Trust: trust, Client: httpClient,
	})
	if err != nil {
		return err
	}
	request := pluginv1.ArtifactResolveRequest{
		Roots:  []pluginv1.ArtifactRequirement{{PluginID: opts.PluginID, Constraint: "=" + opts.Version, Channel: opts.Channel}},
		Target: opts.Target, KernelVersion: opts.KernelVersion, Platform: opts.Platform,
		AllowedChannels: []string{opts.Channel}, AllowedPublishers: splitList(opts.AllowedPublishers), AllowedPluginPrefixes: splitList(opts.AllowedPrefixes),
	}
	if len(request.AllowedPublishers) == 0 {
		return errors.New("allowed-publishers 不能为空")
	}
	lock, err := source.ResolveLock(ctx, request)
	if err != nil {
		return err
	}
	records, err := locallibrary.ImportLock(ctx, source, destination, lock)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"lock": lock, "imports": records})
}

func splitList(raw string) []string {
	var values []string
	seen := map[string]struct{}{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, duplicate := seen[item]; duplicate {
			continue
		}
		seen[item] = struct{}{}
		values = append(values, item)
	}
	return values
}

func readStatus(ctx context.Context, endpoint string) (developmentStatus, error) {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return developmentStatus{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return developmentStatus{}, fmt.Errorf("开发状态端点返回 HTTP %d", response.StatusCode)
	}
	var status developmentStatus
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&status); err != nil {
		return developmentStatus{}, err
	}
	return status, nil
}

func confinedRunDir(stateRoot, raw string) (string, error) {
	stateRoot = filepath.Clean(stateRoot)
	runDir, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	runDir = filepath.Clean(runDir)
	runsRoot := filepath.Join(stateRoot, "runs") + string(os.PathSeparator)
	if !strings.HasPrefix(runDir+string(os.PathSeparator), runsRoot) {
		return "", errors.New("状态端点返回的运行目录越出开发状态根")
	}
	return runDir, nil
}

func readSecret(filename string) (string, error) {
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("凭证文件缺失、不是普通文件或权限过宽: %s", filename)
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if len(value) < 16 {
		return "", errors.New("仓库凭证为空或过短")
	}
	return value, nil
}

func remoteHTTPClient(caFile string, timeout time.Duration) (*http.Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	if caFile != "" {
		raw, err := os.ReadFile(caFile)
		if err != nil || !pool.AppendCertsFromPEM(raw) {
			return nil, errors.New("远端仓库 CA 无效")
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, DialContext: dialer.DialContext, ResponseHeaderTimeout: 30 * time.Second}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("远端仓库禁止重定向") }}, nil
}
