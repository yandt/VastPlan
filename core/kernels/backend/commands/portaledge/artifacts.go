package portaledgecommand

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func buildPortalRemoteArtifactSource(repositoryURL, trustFile, token, caFile string) (*pluginservice.RemoteRepository, *pluginservice.TrustStore, error) {
	if strings.TrimSpace(repositoryURL) == "" {
		if trustFile != "" || token != "" || caFile != "" {
			return nil, nil, errors.New("Portal 远端仓库参数必须与 -repository-url 一起配置")
		}
		return nil, nil, nil
	}
	if strings.TrimSpace(trustFile) == "" || strings.TrimSpace(token) == "" {
		return nil, nil, errors.New("Portal 远端仓库必须配置 trust 和读令牌")
	}
	trust, err := pluginservice.LoadTrustStore(trustFile)
	if err != nil {
		return nil, nil, fmt.Errorf("加载 Portal 远端制品信任: %w", err)
	}
	client, err := portalArtifactHTTPClient(caFile)
	if err != nil {
		return nil, nil, err
	}
	return &pluginservice.RemoteRepository{BaseURL: repositoryURL, Token: token, Trust: trust, Client: client}, trust, nil
}

func portalArtifactHTTPClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(caFile) != "" {
		raw, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("读取 Portal 制品仓库 CA: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("Portal 制品仓库 CA PEM 不包含有效证书")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: transport, Timeout: 2 * time.Minute}, nil
}
