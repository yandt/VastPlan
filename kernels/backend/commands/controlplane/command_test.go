package controlplanecommand

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"strings"
	"testing"
)

func TestRunHelpAndPreflightDoNotConnect(t *testing.T) {
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"-help"}, &output, &output); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help 应由标准 flag 语义返回: %v", err)
	}
	if err := Run(context.Background(), nil, &output, &output); err == nil || !strings.Contains(err.Error(), "-platform-profile") {
		t.Fatalf("缺少发布输入必须在连接 NATS 前失败: %v", err)
	}
	if err := Run(context.Background(), []string{"-platform-profile", "profile.json"}, &output, &output); err == nil || !strings.Contains(err.Error(), "同时提供") {
		t.Fatalf("两份组合输入必须成对提交: %v", err)
	}
	if err := Run(context.Background(), []string{"-desired", "deployment.json"}, &output, &output); err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("旧 raw deployment 发布入口必须直接移除: %v", err)
	}
}
