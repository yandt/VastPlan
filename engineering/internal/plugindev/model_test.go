package plugindev

import (
	"os"
	"path/filepath"
	"testing"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
)

func TestDiscoverUsesManifestAndDetectsCurrentBackendDrivers(t *testing.T) {
	root := repositoryRoot(t)
	for _, test := range []struct {
		id     string
		driver Driver
	}{
		{"cn.vastplan.hello-world", DriverNativeGo},
		{"cn.vastplan.foundation.backend.runtime.node-worker-hello", DriverNodeWorker},
		{"cn.vastplan.python-hello", DriverPython},
		{"cn.vastplan.foundation.security.bootstrap-policy", DriverDynamicGo},
	} {
		spec, err := Discover(root, test.id)
		if err != nil || spec.ID != test.id || spec.Driver != test.driver || spec.Entry == "" {
			t.Fatalf("Discover(%s) = %+v, %v", test.id, spec, err)
		}
	}
}

func TestWorkspaceVersionIsDeterministicAndBoundToStableBase(t *testing.T) {
	version, err := WorkspaceVersion("1.2.3", "0123456789abcdef")
	if err != nil || version != "1.2.3-dev.workspace.0123456789abcdef" {
		t.Fatalf("version=%q err=%v", version, err)
	}
	for _, invalid := range []string{"1.2", "1.2.3-dev.1", "1.2.3+build"} {
		if _, err := WorkspaceVersion(invalid, "0123456789abcdef"); err == nil {
			t.Fatalf("invalid base version accepted: %s", invalid)
		}
	}
}

func TestSourceDigestTracksPluginAndSharedSDKInputs(t *testing.T) {
	root, spec := fixtureRepository(t)
	first, err := SourceDigest(root, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "extensions", "sdk", "go", "sdk.go"), []byte("package sdk\nconst V = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := SourceDigest(root, spec)
	if err != nil || first == second {
		t.Fatalf("shared SDK change not observed: first=%s second=%s err=%v", first, second, err)
	}
}

func TestCommandBuilderBuildsImmutableNativeWorkspaceCandidate(t *testing.T) {
	root := repositoryRoot(t)
	spec, err := Discover(root, "cn.vastplan.demo-quota")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := SourceDigest(root, spec)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := (CommandBuilder{RepositoryRoot: root, StateRoot: t.TempDir(), GoCache: filepath.Join(t.TempDir(), "go-cache")}).Build(t.Context(), spec, digest, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(candidate.PackageFile)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := artifactrepository.Describe("workspace", raw)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.PluginID != spec.ID || artifact.Version != candidate.Version || artifact.Channel != "workspace" {
		t.Fatalf("workspace artifact identity mismatch: artifact=%+v candidate=%+v", artifact, candidate)
	}
}

func fixtureRepository(t *testing.T) (string, Spec) {
	t.Helper()
	root := t.TempDir()
	pluginID := "cn.vastplan.example.dev"
	pluginRoot := filepath.Join(root, "extensions", "plugins", pluginID)
	for _, directory := range []string{
		pluginRoot, filepath.Join(pluginRoot, "backend"), filepath.Join(root, "contracts"),
		filepath.Join(root, "core", "shared", "go"), filepath.Join(root, "extensions", "sdk", "go"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"id":"cn.vastplan.example.dev","name":"Dev","version":"1.0.0","publisher":"vastplan","description":"dev fixture","engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/dev"},"contributes":{"backend":{"tools":[]}}}`
	files := map[string]string{
		filepath.Join(pluginRoot, "vastplan.plugin.json"):        manifest,
		filepath.Join(pluginRoot, "backend", "main.go"):          "package main\nfunc main() {}\n",
		filepath.Join(root, "go.mod"):                            "module example.test/dev\ngo 1.24\n",
		filepath.Join(root, "go.sum"):                            "",
		filepath.Join(root, "contracts", "schema.json"):          "{}\n",
		filepath.Join(root, "core", "shared", "go", "shared.go"): "package shared\n",
		filepath.Join(root, "extensions", "sdk", "go", "sdk.go"): "package sdk\nconst V = 1\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	spec, err := Discover(root, pluginID)
	if err != nil {
		t.Fatal(err)
	}
	return root, spec
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
