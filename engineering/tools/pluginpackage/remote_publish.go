package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

type remotePublishOptions struct {
	RepositoryURL string
	PublishToken  string
	ReadToken     string
	TrustFile     string
	SignKey       string
	KeyID         string
	CAFile        string
	Timeout       time.Duration
	Client        *http.Client
}

func publishRemote(packageBytes []byte, publisher, channel string, options remotePublishOptions) (pluginservice.Artifact, error) {
	if options.RepositoryURL == "" || options.PublishToken == "" || options.TrustFile == "" || options.SignKey == "" || options.KeyID == "" {
		return pluginservice.Artifact{}, errors.New("必须配置远端地址、发布令牌、信任文档、签名私钥和 key ID")
	}
	if channel == "stable" && (options.ReadToken == "" || options.ReadToken == options.PublishToken) {
		return pluginservice.Artifact{}, errors.New("stable 发布必须配置与发布令牌不同的读取令牌")
	}
	if options.Timeout <= 0 {
		return pluginservice.Artifact{}, errors.New("远端发布超时必须大于零")
	}
	artifact, err := pluginservice.Describe(channel, packageBytes)
	if err != nil {
		return pluginservice.Artifact{}, fmt.Errorf("生成制品元数据: %w", err)
	}
	privateKey, err := pluginservice.LoadEd25519PrivateKeyPEM(options.SignKey)
	if err != nil {
		return pluginservice.Artifact{}, fmt.Errorf("加载发布私钥: %w", err)
	}
	trust, err := pluginservice.LoadTrustStore(options.TrustFile)
	if err != nil {
		return pluginservice.Artifact{}, fmt.Errorf("加载信任文档: %w", err)
	}
	client := options.Client
	if client == nil {
		client, err = repositoryHTTPClient(options.CAFile, options.Timeout)
		if err != nil {
			return pluginservice.Artifact{}, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()
	if channel == "stable" {
		reader := &pluginservice.RemoteRepository{BaseURL: options.RepositoryURL, Token: options.ReadToken, Trust: trust, Client: client}
		if err := verifyStableCandidate(ctx, reader, artifact, publisher, options.KeyID); err != nil {
			return pluginservice.Artifact{}, err
		}
	}
	attestation, err := pluginservice.SignArtifact(artifact, publisher, options.KeyID, privateKey, time.Now().UTC())
	if err != nil {
		return pluginservice.Artifact{}, fmt.Errorf("签署制品: %w", err)
	}
	remote := &pluginservice.RemoteRepository{BaseURL: options.RepositoryURL, Token: options.PublishToken, Trust: trust, Client: client}
	published, err := remote.PublishRemote(ctx, attestation, packageBytes)
	if err != nil {
		return pluginservice.Artifact{}, fmt.Errorf("提交签名制品: %w", err)
	}
	return published, nil
}

func verifyStableCandidate(ctx context.Context, reader *pluginservice.RemoteRepository, stable pluginservice.Artifact, publisher, keyID string) error {
	if reader == nil {
		return errors.New("stable 发布缺少 testing 候选读取器")
	}
	source := pluginservice.Ref{PluginID: stable.PluginID, Version: stable.Version, Channel: "testing"}
	envelope, err := reader.Fetch(ctx, source)
	if err != nil {
		return fmt.Errorf("读取已批准的 testing 候选: %w", err)
	}
	if envelope.Artifact.PluginID != stable.PluginID || envelope.Artifact.Version != stable.Version || envelope.Artifact.Channel != "testing" || envelope.Artifact.SHA256 != stable.SHA256 || envelope.Artifact.Size != stable.Size {
		return errors.New("stable 包与 testing 候选的不可变身份或 SHA-256 不一致")
	}
	var proof pluginservice.Attestation
	if err := json.Unmarshal(envelope.Proof, &proof); err != nil {
		return errors.New("testing 候选证明无法解析")
	}
	if proof.Publisher != strings.TrimSpace(publisher) || proof.KeyID != strings.TrimSpace(keyID) {
		return errors.New("stable 发布者或 key ID 与 testing 候选不一致")
	}
	return nil
}

func repositoryHTTPClient(caFile string, timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		raw, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("读取远端仓库 CA: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("远端仓库 CA PEM 不包含有效证书")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}
