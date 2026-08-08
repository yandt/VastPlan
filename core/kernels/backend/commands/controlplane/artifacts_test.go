package controlplanecommand

import (
	"errors"
	"os"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/deploymentcontroller"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
)

type recordingArtifactReader struct {
	artifact pluginv1.Artifact
	err      error
	calls    int
}

func (r *recordingArtifactReader) Read(pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	r.calls++
	return r.artifact, []byte("package"), r.err
}

func TestFallbackArtifactReaderUsesRemoteOnlyForMissingSeedArtifact(t *testing.T) {
	ref := pluginv1.ArtifactRef{PluginID: "cn.example.demo", Version: "1.0.0-dev.1", Channel: "testing"}
	local := &recordingArtifactReader{err: artifacttrust.ErrNotFound}
	remote := &recordingArtifactReader{artifact: pluginv1.Artifact{PluginID: ref.PluginID, Version: ref.Version, Channel: ref.Channel}}
	artifact, _, err := (fallbackArtifactReader{readers: []deploymentcontroller.ArtifactReader{local, remote}}).Read(ref)
	if err != nil || artifact.PluginID != ref.PluginID || local.calls != 1 || remote.calls != 1 {
		t.Fatalf("Seed 缺失时应使用远端精确制品: artifact=%+v local=%d remote=%d err=%v", artifact, local.calls, remote.calls, err)
	}
}

func TestFallbackArtifactReaderDoesNotTreatRawFilesystemMissAsProtocolAbsence(t *testing.T) {
	ref := pluginv1.ArtifactRef{PluginID: "cn.example.demo", Version: "1.0.0", Channel: "stable"}
	local := &recordingArtifactReader{err: os.ErrNotExist}
	remote := &recordingArtifactReader{artifact: pluginv1.Artifact{PluginID: ref.PluginID}}
	if _, _, err := (fallbackArtifactReader{readers: []deploymentcontroller.ArtifactReader{local, remote}}).Read(ref); err == nil || remote.calls != 0 {
		t.Fatalf("未规范化的文件错误不得驱动跨源回退: remote=%d err=%v", remote.calls, err)
	}
}

func TestControllerLocalArtifactReaderNormalizesMissingArtifact(t *testing.T) {
	repository, err := artifactrepository.NewRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref := pluginv1.ArtifactRef{PluginID: "cn.example.demo", Version: "1.0.0", Channel: "stable"}
	_, _, err = (controllerLocalArtifactReader{repository: repository}).Read(ref)
	if !errors.Is(err, artifacttrust.ErrNotFound) {
		t.Fatalf("本地仓库适配器必须在边界规范化精确缺失: %v", err)
	}
}

func TestFallbackArtifactReaderDoesNotHideCorruptSeedArtifact(t *testing.T) {
	ref := pluginv1.ArtifactRef{PluginID: "cn.example.demo", Version: "1.0.0", Channel: "stable"}
	local := &recordingArtifactReader{err: errors.New("seed artifact digest mismatch")}
	remote := &recordingArtifactReader{artifact: pluginv1.Artifact{PluginID: ref.PluginID}}
	if _, _, err := (fallbackArtifactReader{readers: []deploymentcontroller.ArtifactReader{local, remote}}).Read(ref); err == nil || remote.calls != 0 {
		t.Fatalf("Seed 损坏不得静默回退远端: remote=%d err=%v", remote.calls, err)
	}
}
