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
	if err := Run(context.Background(), nil, &output, &output); err == nil || !strings.Contains(err.Error(), "-desired") {
		t.Fatalf("缺少发布输入必须在连接 NATS 前失败: %v", err)
	}
}
