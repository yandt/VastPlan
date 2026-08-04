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

func TestResolverCaretCompatibilityHonorsZeroVersionBoundaries(t *testing.T) {
	entries := []Entry{
		resolverEntry("cn.example.app", "0.4.1", 1, nil),
		resolverEntry("cn.example.app", "0.5.0", 2, nil),
		resolverEntry("cn.example.micro", "0.0.5", 3, nil),
		resolverEntry("cn.example.micro", "0.0.6", 4, nil),
	}
	lock, err := resolveEntries(4, entries, resolverRequest(
		pluginv1.ArtifactRequirement{PluginID: "cn.example.app", Constraint: "^0.4.0"},
		pluginv1.ArtifactRequirement{PluginID: "cn.example.micro", Constraint: "^0.0.5"},
	))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Packages[0].Ref.Version != "0.4.1" || lock.Packages[1].Ref.Version != "0.0.5" {
		t.Fatalf("caret 0.x 兼容边界错误: %+v", lock.Packages)
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

func TestResolverPinsAnExplicitRootChannel(t *testing.T) {
	stable := resolverEntry("cn.example.app", "1.0.0", 1, nil)
	testing := resolverEntry("cn.example.app", "1.0.0", 2, nil)
	testing.Ref.Channel = "testing"
	request := resolverRequest(pluginv1.ArtifactRequirement{PluginID: "cn.example.app", Constraint: "=1.0.0", Channel: "testing"})
	request.AllowedChannels = []string{"stable", "testing"}
	lock, err := resolveEntries(2, []Entry{stable, testing}, request)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Packages[0].Ref.Channel != "testing" || lock.Roots[0].Channel != "testing" {
		t.Fatalf("explicit root channel must be preserved in selection and lock: %#v", lock)
	}
	request.AllowedChannels = []string{"stable"}
	_, err = resolveEntries(2, []Entry{stable, testing}, request)
	assertResolutionCode(t, err, "REQUEST_INVALID")
}

func TestResolverRejectsCyclesAndMissingStrongCapabilities(t *testing.T) {
	cyclic := []Entry{
		resolverEntry("cn.example.a", "1.0.0", 1, map[string]string{"cn.example.b": "^1.0"}),
		resolverEntry("cn.example.b", "1.0.0", 2, map[string]string{"cn.example.a": "^1.0"}),
	}
	_, err := resolveEntries(2, cyclic, resolverRequest(pluginv1.ArtifactRequirement{PluginID: "cn.example.a", Constraint: "^1.0"}))
	assertResolutionCode(t, err, "DEPENDENCY_CYCLE")

	consumer := resolverEntry("cn.example.consumer", "1.0.0", 1, nil)
	consumer.RuntimeRequires = []pluginv1.RuntimeRequirement{{Capability: "platform.database", ContractRange: "^2.0", Scope: "remote", Kind: "strong", Ready: "readiness", FailurePolicy: "fail"}}
	request := resolverRequest(pluginv1.ArtifactRequirement{PluginID: consumer.Ref.PluginID, Constraint: "^1.0"})
	request.AvailableCapabilities = []pluginv1.AvailableCapability{{Capability: "platform.database", ContractVersion: "1.5.0"}}
	_, err = resolveEntries(1, []Entry{consumer}, request)
	assertResolutionCode(t, err, "CAPABILITY_UNSATISFIED")
	request.AvailableCapabilities[0].ContractVersion = "2.1.0"
	if _, err := resolveEntries(1, []Entry{consumer}, request); err != nil {
		t.Fatalf("matching external capability should satisfy the lock: %v", err)
	}
	consumer.RuntimeRequires[0].Kind = "data"
	request.AvailableCapabilities = nil
	_, err = resolveEntries(1, []Entry{consumer}, request)
	assertResolutionCode(t, err, "CAPABILITY_UNSATISFIED")
	consumer.RuntimeRequires[0].Kind = "strong"
	request.AvailableCapabilities = []pluginv1.AvailableCapability{{Capability: "platform.database", ContractVersion: "2.1.0"}}
	consumer.RuntimeRequires[0].Scope = "same-kernel"
	if _, err := resolveEntries(1, []Entry{consumer}, request); err == nil {
		t.Fatal("external capability must not satisfy a local runtime requirement")
	}
	provider := resolverEntry("cn.example.database", "2.2.0", 2, nil)
	provider.RuntimeProvides = []pluginv1.RuntimeCapabilityPolicy{{ExtensionPoint: "tool.package", Capability: "platform.database", ContractVersion: "2.2.0"}}
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

func TestResolverBacktracksAcrossCandidateFeatureDefinitions(t *testing.T) {
	high := resolverEntry("cn.example.app", "1.1.0", 2, nil)
	low := resolverEntry("cn.example.app", "1.0.0", 1, nil)
	low.CompositionFeatures = map[string]pluginv1.CompositionFeature{
		"audit": {ID: "audit", Dependencies: map[string]string{"cn.example.library": "^1.0"}},
	}
	library := resolverEntry("cn.example.library", "1.5.0", 3, nil)
	request := resolverRequest(pluginv1.ArtifactRequirement{PluginID: high.Ref.PluginID, Constraint: "^1.0", Features: []string{"audit"}})
	lock, err := resolveEntries(3, []Entry{high, low, library}, request)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Packages[0].Ref.Version != "1.0.0" || lock.Packages[0].Dependencies["cn.example.library"] != "^1.0" {
		t.Fatalf("Resolver 未回退到声明 Feature 的候选或未锁定有效依赖: %+v", lock.Packages)
	}
	request.Roots[0].Features = []string{"missing"}
	_, err = resolveEntries(3, []Entry{high, low, library}, request)
	assertResolutionCode(t, err, "FEATURE_UNAVAILABLE")
}

func TestResolverBacktracksWhenHighestFeatureDependencyConflicts(t *testing.T) {
	high := resolverEntry("cn.example.app", "1.1.0", 2, nil)
	high.CompositionFeatures = map[string]pluginv1.CompositionFeature{
		"audit": {ID: "audit", Dependencies: map[string]string{"cn.example.library": "^2.0"}},
	}
	low := resolverEntry("cn.example.app", "1.0.0", 1, nil)
	low.CompositionFeatures = map[string]pluginv1.CompositionFeature{
		"audit": {ID: "audit", Dependencies: map[string]string{"cn.example.library": "^1.0"}},
	}
	library := resolverEntry("cn.example.library", "1.5.0", 3, nil)
	request := resolverRequest(
		pluginv1.ArtifactRequirement{PluginID: high.Ref.PluginID, Constraint: "^1.0", Features: []string{"audit"}},
		pluginv1.ArtifactRequirement{PluginID: library.Ref.PluginID, Constraint: "^1.0"},
	)
	lock, err := resolveEntries(3, []Entry{high, low, library}, request)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Packages[0].Ref.Version != "1.0.0" {
		t.Fatalf("Feature 依赖冲突时未回退根候选: %+v", lock.Packages)
	}
}

func TestResolverUnionsFeatureDependenciesAndCapabilities(t *testing.T) {
	app := resolverEntry("cn.example.app", "1.0.0", 1, nil)
	app.CompositionFeatures = map[string]pluginv1.CompositionFeature{
		"audit": {
			ID: "audit", Dependencies: map[string]string{"cn.example.audit": "^1.0"},
			RuntimeRequires: []pluginv1.RuntimeRequirement{{Capability: "platform.database", ContractRange: "^2.0", Scope: "remote", Kind: "strong", Ready: "readiness", FailurePolicy: "fail"}},
		},
		"quota": {ID: "quota", Dependencies: map[string]string{"cn.example.quota": "^1.0"}},
	}
	audit := resolverEntry("cn.example.audit", "1.1.0", 2, nil)
	quota := resolverEntry("cn.example.quota", "1.2.0", 3, nil)
	request := resolverRequest(pluginv1.ArtifactRequirement{PluginID: app.Ref.PluginID, Constraint: "=1.0.0", Features: []string{"quota", "audit"}})
	request.AvailableCapabilities = []pluginv1.AvailableCapability{{Capability: "platform.database", ContractVersion: "1.9.0"}}
	_, err := resolveEntries(3, []Entry{app, audit, quota}, request)
	assertResolutionCode(t, err, "CAPABILITY_UNSATISFIED")
	request.AvailableCapabilities[0].ContractVersion = "2.1.0"
	lock, err := resolveEntries(3, []Entry{app, audit, quota}, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packages) != 3 || len(lock.Packages[0].Dependencies) != 2 || !equalFeatureIDs(lock.Roots[0].Features, []string{"audit", "quota"}) {
		t.Fatalf("Feature 并集或有效闭包错误: %+v", lock)
	}
}

func TestResolverRejectsFeatureCyclesAndBoundsSearch(t *testing.T) {
	app := resolverEntry("cn.example.app", "1.0.0", 1, nil)
	app.CompositionFeatures = map[string]pluginv1.CompositionFeature{
		"cycle": {ID: "cycle", Dependencies: map[string]string{"cn.example.worker": "^1.0"}},
	}
	worker := resolverEntry("cn.example.worker", "1.0.0", 2, map[string]string{"cn.example.app": "^1.0"})
	request := resolverRequest(pluginv1.ArtifactRequirement{PluginID: app.Ref.PluginID, Constraint: "^1.0", Features: []string{"cycle"}})
	_, err := resolveEntries(2, []Entry{app, worker}, request)
	assertResolutionCode(t, err, "DEPENDENCY_CYCLE")

	app.CompositionFeatures = nil
	app.Dependencies = map[string]string{"cn.example.worker": "^1.0"}
	worker.Dependencies = nil
	_, err = resolveEntriesWithBudget(2, []Entry{app, worker}, resolverRequest(pluginv1.ArtifactRequirement{PluginID: app.Ref.PluginID, Constraint: "^1.0"}), 1)
	assertResolutionCode(t, err, "RESOLUTION_COMPLEXITY_LIMIT")
}

func equalFeatureIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
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
