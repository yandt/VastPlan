package artifacttrust

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestResolveExactFallsBackOnlyOnArtifactNotFound(t *testing.T) {
	ref := pluginv1.ArtifactRef{PluginID: "com.example.plugin", Version: "1.0.0", Channel: "stable"}
	calls := 0
	resolved, err := ResolveExact(context.Background(), ref, []string{"seed", "remote"}, func(source string) string { return source },
		func(_ context.Context, source string, _ pluginv1.ArtifactRef) (string, error) {
			calls++
			if source == "seed" {
				return "", ErrNotFound
			}
			return "verified", nil
		})
	if err != nil || resolved != "verified" || calls != 2 {
		t.Fatalf("精确缺失应按顺序换源: resolved=%q calls=%d err=%v", resolved, calls, err)
	}
}

func TestResolveExactFailsClosedForRawFilesystemMissAndVerificationError(t *testing.T) {
	ref := pluginv1.ArtifactRef{PluginID: "com.example.plugin", Version: "1.0.0", Channel: "stable"}
	for _, failure := range []error{os.ErrNotExist, errors.New("publisher proof invalid")} {
		secondCalls := 0
		_, err := ResolveExact(context.Background(), ref, []string{"seed", "remote"}, func(source string) string { return source },
			func(_ context.Context, source string, _ pluginv1.ArtifactRef) (string, error) {
				if source == "seed" {
					return "", failure
				}
				secondCalls++
				return "verified", nil
			})
		if err == nil || !strings.Contains(err.Error(), failure.Error()) || secondCalls != 0 {
			t.Fatalf("非协议缺失错误必须立即失败: failure=%v calls=%d err=%v", failure, secondCalls, err)
		}
	}
}

func TestResolveExactPreservesNotFoundIdentityAndRejectsIncompleteCandidates(t *testing.T) {
	ref := pluginv1.ArtifactRef{PluginID: "com.example.plugin", Version: "1.0.0", Channel: "stable"}
	_, err := ResolveExact(context.Background(), ref, []string{"seed"}, func(source string) string { return source },
		func(context.Context, string, pluginv1.ArtifactRef) (string, error) { return "", ErrNotFound })
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("全部缺失必须保留统一错误身份: %v", err)
	}
	if _, err := ResolveExact[string, string](context.Background(), ref, []string{"seed"}, nil, nil); err == nil {
		t.Fatal("缺少协议回调的候选源必须拒绝")
	}
	if _, err := ResolveExact(context.Background(), ref, []string(nil), func(source string) string { return source },
		func(context.Context, string, pluginv1.ArtifactRef) (string, error) { return "", nil }); err == nil {
		t.Fatal("空候选源集合必须拒绝")
	}
}
