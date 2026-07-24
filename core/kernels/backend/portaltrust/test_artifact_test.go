package portaltrust

import (
	"context"
	"errors"
	"strings"
	"testing"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type staticTestArtifactIndex struct {
	receipt artifactrepositoryv1.Receipt
	err     error
}

func (i staticTestArtifactIndex) Validate(_ context.Context, receipt artifactrepositoryv1.Receipt) error {
	if i.err != nil {
		return i.err
	}
	if receipt != i.receipt {
		return errors.New("unexpected receipt")
	}
	return nil
}

func TestTrustedCatalogValidatesFrontendTestingReceiptAgainstPackageAndJournal(t *testing.T) {
	manifest := `{"id":"cn.vastplan.product.frontend.receipt","name":"receipt","description":"test","version":"1.1.0-dev.20260721.1.abcdef0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"views":[]}}}`
	artifact, packageBytes := packageFrontendFixture(t, manifest, []byte(`export default { register() {} };`))
	artifact.Channel = "testing"
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	receipt := artifactrepositoryv1.Receipt{SchemaVersion: 1, RepositoryID: "local-testing", Protocol: artifactrepositoryv1.ProtocolLocalTest, ProfileDigest: strings.Repeat("a", 64), Ref: ref, SHA256: artifact.SHA256, Revision: 23}
	index := staticTestArtifactIndex{receipt: receipt}
	catalog, err := NewTrustedCatalog(
		[]ArtifactSource{catalogSource{artifact.PluginID + "@" + artifact.Version: artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes}}},
		contentVerifier{}, WithTestArtifactIndex(index),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := portalapi.CreateTestReleaseRequest{BindingID: "ui", Receipt: receipt}
	if err := catalog.ValidateTestArtifact(context.Background(), "tenant-a", request, []string{"vastplan"}); err != nil {
		t.Fatalf("精确 Frontend testing 回执应通过: %v", err)
	}
	request.Receipt.SHA256 = strings.Repeat("f", 64)
	if err := catalog.ValidateTestArtifact(context.Background(), "tenant-a", request, []string{"vastplan"}); err == nil {
		t.Fatal("仓库回执摘要与已验证包不一致必须拒绝")
	}
}
