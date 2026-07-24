package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactrepository/localtest"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

func publishLocalTest(
	ctx context.Context,
	runDir, stateRoot string,
	profile artifactrepositoryv1.Profile,
	trust *pluginservice.TrustStore,
	manifest pluginv1.Manifest,
	artifact pluginservice.Artifact,
	packageBytes []byte,
) (pluginservice.Artifact, artifactrepositoryv1.Receipt, bool, error) {
	token, err := localtest.ReadTokenFile(filepath.Join(runDir, "secrets", "artifact-local-test.token"))
	if err != nil {
		return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, err
	}
	client, err := localtest.NewClient(profile, token)
	if err != nil {
		return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, err
	}
	defer client.CloseIdleConnections()
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	snapshot, err := client.CatalogSnapshot(ctx)
	if err != nil {
		return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, fmt.Errorf("读取 local-test Catalog: %w", err)
	}
	if receipt, found := exactReceipt(snapshot, ref); found {
		if receipt.SHA256 != artifact.SHA256 {
			return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, errors.New("local-test 仓库已存在相同 ref 但摘要不同的不可变制品")
		}
		envelope, err := client.ReadExact(ctx, ref)
		if err != nil {
			return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, err
		}
		if err := verifyLocalEnvelope(trust, envelope); err != nil {
			return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, err
		}
		return envelope.Artifact, receipt, true, nil
	}
	privateKeyFile := filepath.Join(stateRoot, "repositories", "testing", "secrets", "artifact-signing.pem")
	if err := requireRegularFile(privateKeyFile, true); err != nil {
		return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, err
	}
	privateKey, err := pluginservice.LoadEd25519PrivateKeyPEM(privateKeyFile)
	if err != nil {
		return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, err
	}
	attestation, err := pluginservice.SignArtifact(artifact, manifest.Publisher, "local-testing", privateKey, time.Now().UTC())
	if err != nil {
		return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, err
	}
	proof, err := json.Marshal(attestation)
	if err != nil {
		return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, err
	}
	receipt, err := client.Publish(ctx, artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes, Proof: proof})
	if err != nil {
		return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, err
	}
	envelope, err := client.ReadExact(ctx, ref)
	if err != nil {
		return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, err
	}
	if err := verifyLocalEnvelope(trust, envelope); err != nil {
		return pluginservice.Artifact{}, artifactrepositoryv1.Receipt{}, false, err
	}
	return envelope.Artifact, receipt, false, nil
}

func exactReceipt(snapshot artifactrepositoryv1.CatalogSnapshot, ref pluginv1.ArtifactRef) (artifactrepositoryv1.Receipt, bool) {
	for _, receipt := range snapshot.Items {
		if receipt.Ref == ref {
			return receipt, true
		}
	}
	return artifactrepositoryv1.Receipt{}, false
}

func verifyLocalEnvelope(trust *pluginservice.TrustStore, envelope artifacttrust.Envelope) error {
	if err := trust.VerifyProof(envelope); err != nil {
		return fmt.Errorf("local-test 制品证明不可信: %w", err)
	}
	if err := artifacttrust.ValidateContent(envelope.Artifact, envelope.PackageBytes); err != nil {
		return fmt.Errorf("local-test 制品内容无效: %w", err)
	}
	return nil
}
