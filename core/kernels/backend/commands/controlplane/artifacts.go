package controlplanecommand

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/deploymentcontroller"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactrepository/localtest"
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

type controllerRepositoryOptions struct {
	URL, ProfileFile, TrustFile, Token, TokenFile, CAFile string
}

func buildControllerArtifactReader(local *pluginservice.Repository, options controllerRepositoryOptions) (deploymentcontroller.ArtifactReader, error) {
	if local == nil {
		return nil, errors.New("controller 本地 Seed 制品源不能为空")
	}
	if options.URL != "" && options.ProfileFile != "" {
		return nil, errors.New("controller repository URL 与 Profile 不能同时配置")
	}
	if strings.TrimSpace(options.URL) == "" && strings.TrimSpace(options.ProfileFile) == "" {
		if options.TrustFile != "" || options.Token != "" || options.TokenFile != "" || options.CAFile != "" {
			return nil, errors.New("controller 仓库凭证参数必须与 URL 或 Profile 一起配置")
		}
		return local, nil
	}
	if strings.TrimSpace(options.TrustFile) == "" {
		return nil, errors.New("controller 托管仓库必须配置 trust")
	}
	trust, err := pluginservice.LoadTrustStore(options.TrustFile)
	if err != nil {
		return nil, fmt.Errorf("加载 controller 制品信任: %w", err)
	}
	if options.ProfileFile != "" {
		if options.Token != "" || options.CAFile != "" || options.TokenFile == "" {
			return nil, errors.New("controller local-test Profile 必须只配置 token file")
		}
		profile, err := artifactrepositoryv1.ParseProfileFile(options.ProfileFile)
		if err != nil {
			return nil, err
		}
		if profile.Protocol != artifactrepositoryv1.ProtocolLocalTest {
			return nil, errors.New("controller Profile 当前只接受 local-test.v1")
		}
		token, err := localtest.ReadTokenFile(options.TokenFile)
		if err != nil {
			return nil, err
		}
		registry := artifactrepository.NewRegistry()
		if err := registry.Register(profile.Protocol, localtest.Factory(token)); err != nil {
			return nil, err
		}
		adapter, err := registry.Open(profile)
		if err != nil {
			return nil, err
		}
		return fallbackArtifactReader{readers: []deploymentcontroller.ArtifactReader{local, trustedProtocolReader{adapter: adapter, trust: trust}}}, nil
	}
	if options.Token == "" || options.TokenFile != "" {
		return nil, errors.New("controller remote 仓库必须只配置读 token")
	}
	client, err := controllerArtifactHTTPClient(options.CAFile)
	if err != nil {
		return nil, err
	}
	remote := &pluginservice.RemoteRepository{BaseURL: options.URL, Token: options.Token, Trust: trust, Client: client}
	return fallbackArtifactReader{readers: []deploymentcontroller.ArtifactReader{local, remote}}, nil
}

type trustedProtocolReader struct {
	adapter artifactrepository.Adapter
	trust   *pluginservice.TrustStore
}

func (r trustedProtocolReader) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	envelope, err := r.adapter.ReadExact(context.Background(), ref)
	if err != nil {
		return pluginv1.Artifact{}, nil, err
	}
	if err := r.trust.VerifyProof(envelope); err != nil {
		return pluginv1.Artifact{}, nil, fmt.Errorf("controller 仓库证明不可信: %w", err)
	}
	if err := artifacttrust.ValidateContent(envelope.Artifact, envelope.PackageBytes); err != nil {
		return pluginv1.Artifact{}, nil, fmt.Errorf("controller 仓库内容无效: %w", err)
	}
	return envelope.Artifact, envelope.PackageBytes, nil
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
