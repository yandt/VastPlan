package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
)

type assemblyArtifactSource struct {
	envelope artifacttrust.Envelope
	err      error
	calls    int
}

func (s *assemblyArtifactSource) Fetch(context.Context, pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	s.calls++
	return s.envelope, s.err
}

func TestArtifactResolutionUsesSharedStrictSourceTraversal(t *testing.T) {
	artifact, packageBytes := assemblyArtifact(t)
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	remote := &assemblyArtifactSource{envelope: artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes}}
	missing := &assemblyArtifactSource{err: artifacttrust.ErrNotFound}
	resolution := artifactResolution{
		sources:  []nodeagent.ArtifactSource{missing, remote},
		verifier: nodeagent.NewLocalDevelopmentArtifactVerifier(),
	}
	resolved, _, err := resolution.Read(ref)
	if err != nil || resolved.SHA256 != artifact.SHA256 || missing.calls != 1 || remote.calls != 1 {
		t.Fatalf("组合解析应只在协议缺失时换源: resolved=%+v missing=%d remote=%d err=%v", resolved, missing.calls, remote.calls, err)
	}

	rawFilesystemMiss := &assemblyArtifactSource{err: os.ErrNotExist}
	remote.calls = 0
	resolution.sources = []nodeagent.ArtifactSource{rawFilesystemMiss, remote}
	if _, _, err := resolution.Read(ref); err == nil || remote.calls != 0 {
		t.Fatalf("组合解析不得把原始文件错误视为跨源缺失: remote=%d err=%v", remote.calls, err)
	}
}

func assemblyArtifact(t *testing.T) (pluginv1.Artifact, []byte) {
	t.Helper()
	root := t.TempDir()
	manifest := []byte(`{
		"id":"com.example.assembly-source","name":"assembly","description":"test","version":"1.0.0","publisher":"example",
		"engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}
	}`)
	if err := os.WriteFile(filepath.Join(root, "vastplan.plugin.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "backend"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "main"), []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	packageBytes, _, err := artifactrepository.PackageDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := artifactrepository.Describe("stable", packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	return artifact, packageBytes
}
