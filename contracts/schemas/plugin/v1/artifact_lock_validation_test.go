package pluginv1

import (
	"strings"
	"testing"
)

func TestValidateArtifactLockSemanticsChecksClosedGraph(t *testing.T) {
	dependency := ArtifactLockPackage{
		Ref:    ArtifactRef{PluginID: "cn.example.dependency", Version: "2.1.0", Channel: "stable"},
		SHA256: strings.Repeat("a", 64), Size: 1, Publisher: "example", KeyID: "release", RepositoryRevision: 4,
	}
	root := ArtifactLockPackage{
		Ref:    ArtifactRef{PluginID: "cn.example.root", Version: "1.0.0", Channel: "stable"},
		SHA256: strings.Repeat("b", 64), Size: 1, Publisher: "example", KeyID: "release", RepositoryRevision: 5,
		Dependencies: map[string]string{dependency.Ref.PluginID: "^2.0.0"},
	}
	lock := ArtifactLock{
		SchemaVersion: "v1", RepositoryRevision: 5, Target: "backend", KernelVersion: "0.1.0",
		Roots:    []ArtifactRequirement{{PluginID: root.Ref.PluginID, Constraint: "=1.0.0", Channel: "stable"}},
		Packages: []ArtifactLockPackage{dependency, root},
	}
	lock.Digest, _ = ArtifactLockDigest(lock)
	if err := ValidateArtifactLockSemantics(lock); err != nil {
		t.Fatalf("合法闭包应通过: %v", err)
	}

	missing := lock
	missing.Packages = []ArtifactLockPackage{root}
	missing.Digest, _ = ArtifactLockDigest(missing)
	if err := ValidateArtifactLockSemantics(missing); err == nil {
		t.Fatal("缺失传递依赖必须拒绝")
	}

	cycle := lock
	cycle.Packages = append([]ArtifactLockPackage(nil), lock.Packages...)
	cycle.Packages[0].Dependencies = map[string]string{root.Ref.PluginID: "^1.0.0"}
	cycle.Digest, _ = ArtifactLockDigest(cycle)
	if err := ValidateArtifactLockSemantics(cycle); err == nil {
		t.Fatal("依赖环必须拒绝")
	}
}
