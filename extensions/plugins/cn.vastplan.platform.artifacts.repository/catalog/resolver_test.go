package catalog

import (
	"errors"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestResolverSelectsHighestCompatibleDependencyAndProducesStableLock(t *testing.T) {
	entries := []Entry{
		resolverEntry("cn.example.app", "1.0.0", 1, map[string]string{"cn.example.library": "^1.0"}),
		resolverEntry("cn.example.app", "1.1.0", 2, map[string]string{"cn.example.library": "^2.0"}),
		resolverEntry("cn.example.library", "1.5.0", 3, nil),
		resolverEntry("cn.example.library", "2.1.0", 4, nil),
	}
	request := resolverRequest(pluginv1.ArtifactRequirement{PluginID: "cn.example.app", Constraint: "^1.0"})
	first, err := resolveEntries(4, entries, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveEntries(4, entries, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || first.RepositoryRevision != 4 || len(first.Packages) != 2 {
		t.Fatalf("lock must be deterministic and complete: %#v %#v", first, second)
	}
	if first.Packages[0].Ref.Version != "1.1.0" || first.Packages[1].Ref.Version != "2.1.0" {
		t.Fatalf("resolver did not choose the highest compatible solution: %#v", first.Packages)
	}
	if err := ValidateLock(first); err != nil {
		t.Fatalf("generated lock must validate: %v", err)
	}
}

func TestResolverBacktracksOnRootConflict(t *testing.T) {
	entries := []Entry{
		resolverEntry("cn.example.app", "1.0.0", 1, map[string]string{"cn.example.library": "^1.0"}),
		resolverEntry("cn.example.app", "1.1.0", 2, map[string]string{"cn.example.library": "^2.0"}),
		resolverEntry("cn.example.library", "1.5.0", 3, nil),
		resolverEntry("cn.example.library", "2.1.0", 4, nil),
	}
	request := resolverRequest(
		pluginv1.ArtifactRequirement{PluginID: "cn.example.app", Constraint: "^1.0"},
		pluginv1.ArtifactRequirement{PluginID: "cn.example.library", Constraint: "^1.0"},
	)
	lock, err := resolveEntries(4, entries, request)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Packages[0].Ref.Version != "1.0.0" || lock.Packages[1].Ref.Version != "1.5.0" {
		t.Fatalf("resolver should backtrack to the compatible app version: %#v", lock.Packages)
	}
}

func TestResolverRejectsCyclesAndMissingStrongCapabilities(t *testing.T) {
	cyclic := []Entry{
		resolverEntry("cn.example.a", "1.0.0", 1, map[string]string{"cn.example.b": "^1.0"}),
		resolverEntry("cn.example.b", "1.0.0", 2, map[string]string{"cn.example.a": "^1.0"}),
	}
	_, err := resolveEntries(2, cyclic, resolverRequest(pluginv1.ArtifactRequirement{PluginID: "cn.example.a", Constraint: "^1.0"}))
	assertResolutionCode(t, err, "DEPENDENCY_CYCLE")

	consumer := resolverEntry("cn.example.consumer", "1.0.0", 1, nil)
	consumer.RuntimeRequires = []pluginv1.RuntimeRequirement{{Capability: "platform.database", Version: "^2.0", Scope: "remote", Kind: "strong", Ready: "readiness", FailurePolicy: "fail"}}
	request := resolverRequest(pluginv1.ArtifactRequirement{PluginID: consumer.Ref.PluginID, Constraint: "^1.0"})
	request.AvailableCapabilities = []pluginv1.AvailableCapability{{Capability: "platform.database", Version: "1.5.0"}}
	_, err = resolveEntries(1, []Entry{consumer}, request)
	assertResolutionCode(t, err, "CAPABILITY_UNSATISFIED")
	request.AvailableCapabilities[0].Version = "2.1.0"
	if _, err := resolveEntries(1, []Entry{consumer}, request); err != nil {
		t.Fatalf("matching external capability should satisfy the lock: %v", err)
	}
	consumer.RuntimeRequires[0].Kind = "data"
	request.AvailableCapabilities = nil
	_, err = resolveEntries(1, []Entry{consumer}, request)
	assertResolutionCode(t, err, "CAPABILITY_UNSATISFIED")
	consumer.RuntimeRequires[0].Kind = "strong"
	request.AvailableCapabilities = []pluginv1.AvailableCapability{{Capability: "platform.database", Version: "2.1.0"}}
	consumer.RuntimeRequires[0].Scope = "same-kernel"
	if _, err := resolveEntries(1, []Entry{consumer}, request); err == nil {
		t.Fatal("external capability must not satisfy a local runtime requirement")
	}
	provider := resolverEntry("cn.example.database", "2.2.0", 2, nil)
	provider.ProvidedCapabilities = []string{"platform.database"}
	request.Roots = append(request.Roots, pluginv1.ArtifactRequirement{PluginID: provider.Ref.PluginID, Constraint: "^2.0"})
	if _, err := resolveEntries(2, []Entry{consumer, provider}, request); err != nil {
		t.Fatalf("selected same-kernel provider should satisfy capability: %v", err)
	}
}

func TestResolverHonorsSnapshotPlatformAndPublisherPolicy(t *testing.T) {
	old := resolverEntry("cn.example.app", "1.0.0", 1, nil)
	old.Platforms = []string{"linux/amd64"}
	latest := resolverEntry("cn.example.app", "2.0.0", 2, nil)
	latest.Platforms = []string{"linux/amd64"}
	request := resolverRequest(pluginv1.ArtifactRequirement{PluginID: old.Ref.PluginID, Constraint: ">=1.0.0"})
	request.SnapshotRevision = 1
	lock, err := resolveEntries(2, []Entry{old, latest}, request)
	if err != nil || lock.Packages[0].Ref.Version != "1.0.0" {
		t.Fatalf("snapshot must exclude later publications: lock=%#v err=%v", lock, err)
	}
	request.Platform = "darwin/arm64"
	_, err = resolveEntries(2, []Entry{old, latest}, request)
	assertResolutionCode(t, err, "VERSION_CONFLICT")
	request.Platform = "linux/amd64"
	request.AllowedPublishers = []string{"other"}
	_, err = resolveEntries(2, []Entry{old, latest}, request)
	assertResolutionCode(t, err, "VERSION_CONFLICT")
}

func resolverEntry(id, version string, revision uint64, dependencies map[string]string) Entry {
	return Entry{
		Ref:    pluginv1.ArtifactRef{PluginID: id, Version: version, Channel: "stable"},
		SHA256: strings.Repeat("a", 64), Size: 1, Publisher: "vastplan", KeyID: "release",
		RepositoryRevision: revision, Engines: map[string]string{"backend": "^0.1"}, Dependencies: dependencies,
	}
}

func resolverRequest(roots ...pluginv1.ArtifactRequirement) pluginv1.ArtifactResolveRequest {
	return pluginv1.ArtifactResolveRequest{
		Roots: roots, Target: "backend", KernelVersion: "0.1.0", Platform: "linux/amd64",
		AllowedChannels: []string{"stable"}, AllowedPublishers: []string{"vastplan"}, AllowedPluginPrefixes: []string{"cn.example"},
	}
}

func assertResolutionCode(t *testing.T, err error, code string) {
	t.Helper()
	var resolution *ResolutionError
	if !errors.As(err, &resolution) || resolution.Code != code {
		t.Fatalf("resolution error=%v, want code %s", err, code)
	}
}
