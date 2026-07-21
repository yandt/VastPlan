package portaltrust

import (
	"context"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type staticTestArtifactIndex struct {
	entry TestArtifactIndexEntry
	err   error
}

func (i staticTestArtifactIndex) Lookup(context.Context, pluginv1.ArtifactRef) (TestArtifactIndexEntry, error) {
	return i.entry, i.err
}

func TestTrustedCatalogValidatesFrontendTestingReceiptAgainstPackageAndJournal(t *testing.T) {
	manifest := `{"id":"cn.vastplan.product.frontend.receipt","name":"receipt","description":"test","version":"1.1.0-dev.20260721.1.abcdef0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"views":[]}}}`
	artifact, packageBytes := packageFrontendFixture(t, manifest, []byte(`export default { register() {} };`))
	artifact.Channel = "testing"
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	index := staticTestArtifactIndex{entry: TestArtifactIndexEntry{Ref: ref, SHA256: artifact.SHA256, Publisher: "vastplan", RepositoryRevision: 23, Targets: []string{"frontend"}}}
	catalog, err := NewTrustedCatalog(
		[]ArtifactSource{catalogSource{artifact.PluginID + "@" + artifact.Version: artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes}}},
		contentVerifier{}, WithTestArtifactIndex(index),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := portalapi.CreateTestReleaseRequest{BindingID: "ui", Artifact: ref, SHA256: artifact.SHA256, RepositoryRevision: 23}
	if err := catalog.ValidateTestArtifact(context.Background(), "tenant-a", request, []string{"vastplan"}); err != nil {
		t.Fatalf("精确 Frontend testing 回执应通过: %v", err)
	}
	request.SHA256 = strings.Repeat("f", 64)
	if err := catalog.ValidateTestArtifact(context.Background(), "tenant-a", request, []string{"vastplan"}); err == nil {
		t.Fatal("仓库回执摘要与已验证包不一致必须拒绝")
	}
}
