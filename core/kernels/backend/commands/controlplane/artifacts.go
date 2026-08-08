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
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository/localtest"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
)

type fallbackArtifactReader struct {
	readers []deploymentcontroller.ArtifactReader
}

type artifactReadResult struct {
	artifact     pluginv1.Artifact
	packageBytes []byte
}

func (r fallbackArtifactReader) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	resolved, err := artifacttrust.ResolveExact(context.Background(), ref, r.readers,
		func(reader deploymentcontroller.ArtifactReader) string {
			if reader == nil {
				return ""
			}
			return fmt.Sprintf("%T", reader)
		},
		func(_ context.Context, reader deploymentcontroller.ArtifactReader, ref pluginv1.ArtifactRef) (artifactReadResult, error) {
			artifact, packageBytes, err := reader.Read(ref)
			return artifactReadResult{artifact: artifact, packageBytes: packageBytes}, err
		})
	if err != nil {
		return pluginv1.Artifact{}, nil, fmt.Errorf("controller 解析精确制品: %w", err)
	}
	return resolved.artifact, resolved.packageBytes, nil
}

type controllerLocalArtifactReader struct {
	repository *artifactrepository.Repository
}

func (r controllerLocalArtifactReader) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	envelope, err := r.repository.Fetch(context.Background(), ref)
	return envelope.Artifact, envelope.PackageBytes, err
}

type controllerRepositoryOptions struct {
	URL, ProfileFile, TrustFile, Token, TokenFile, CAFile string
}

func buildControllerArtifactReader(local *artifactrepository.Repository, options controllerRepositoryOptions) (deploymentcontroller.ArtifactReader, error) {
	if local == nil {
		return nil, errors.New("controller 本地 Seed 制品源不能为空")
	}
	localReader := controllerLocalArtifactReader{repository: local}
	if options.URL != "" && options.ProfileFile != "" {
		return nil, errors.New("controller repository URL 与 Profile 不能同时配置")
	}
	if strings.TrimSpace(options.URL) == "" && strings.TrimSpace(options.ProfileFile) == "" {
		if options.TrustFile != "" || options.Token != "" || options.TokenFile != "" || options.CAFile != "" {
			return nil, errors.New("controller 仓库凭证参数必须与 URL 或 Profile 一起配置")
		}
		return localReader, nil
	}
	if strings.TrimSpace(options.TrustFile) == "" {
		return nil, errors.New("controller 托管仓库必须配置 trust")
	}
	trust, err := artifactrepository.LoadTrustStore(options.TrustFile)
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
		return fallbackArtifactReader{readers: []deploymentcontroller.ArtifactReader{localReader, trustedProtocolReader{adapter: adapter, trust: trust}}}, nil
	}
	if options.Token == "" || options.TokenFile != "" {
		return nil, errors.New("controller remote 仓库必须只配置读 token")
	}
	client, err := controllerArtifactHTTPClient(options.CAFile)
	if err != nil {
		return nil, err
	}
	remote := &artifactrepository.RemoteRepository{BaseURL: options.URL, Token: options.Token, Trust: trust, Client: client}
	return fallbackArtifactReader{readers: []deploymentcontroller.ArtifactReader{localReader, remote}}, nil
}

type trustedProtocolReader struct {
	adapter artifactrepository.Adapter
	trust   *artifactrepository.TrustStore
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
