package controlplanecommand

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/deploymentcontroller"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

type fallbackArtifactReader struct {
	readers []deploymentcontroller.ArtifactReader
}

func (r fallbackArtifactReader) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	var notFound error
	for _, reader := range r.readers {
		if reader == nil {
			return pluginv1.Artifact{}, nil, errors.New("controller 制品源不能为空")
		}
		artifact, packageBytes, err := reader.Read(ref)
		if errors.Is(err, artifacttrust.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			notFound = errors.Join(notFound, err)
			continue
		}
		if err != nil {
			return pluginv1.Artifact{}, nil, err
		}
		return artifact, packageBytes, nil
	}
	return pluginv1.Artifact{}, nil, fmt.Errorf("controller 所有制品源均无精确引用 %s@%s/%s: %w", ref.PluginID, ref.Version, ref.Channel, coalesceArtifactError(notFound, artifacttrust.ErrNotFound))
}

func buildControllerArtifactReader(local *pluginservice.Repository, repositoryURL, trustFile, token, caFile string) (deploymentcontroller.ArtifactReader, error) {
	if local == nil {
		return nil, errors.New("controller 本地 Seed 制品源不能为空")
	}
	if strings.TrimSpace(repositoryURL) == "" {
		if trustFile != "" || token != "" || caFile != "" {
			return nil, errors.New("controller 远端仓库参数必须与 -repository-url 一起配置")
		}
		return local, nil
	}
	if strings.TrimSpace(trustFile) == "" || strings.TrimSpace(token) == "" {
		return nil, errors.New("controller 远端仓库必须配置 trust 和读令牌")
	}
	trust, err := pluginservice.LoadTrustStore(trustFile)
	if err != nil {
		return nil, fmt.Errorf("加载 controller 制品信任: %w", err)
	}
	client, err := controllerArtifactHTTPClient(caFile)
	if err != nil {
		return nil, err
	}
	remote := &pluginservice.RemoteRepository{BaseURL: repositoryURL, Token: token, Trust: trust, Client: client}
	return fallbackArtifactReader{readers: []deploymentcontroller.ArtifactReader{local, remote}}, nil
}

func controllerArtifactHTTPClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(caFile) != "" {
		raw, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("读取 controller 制品仓库 CA: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("controller 制品仓库 CA PEM 不包含有效证书")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: transport, Timeout: 2 * time.Minute}, nil
}

func coalesceArtifactError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}
