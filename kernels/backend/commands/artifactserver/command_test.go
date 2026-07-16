package artifactservercommand

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"strings"
	"testing"
)

func TestRunHelpAndPreflightDoNotStartServer(t *testing.T) {
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"-help"}, &output); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help 应由标准 flag 语义返回: %v", err)
	}
	if err := Run(context.Background(), nil, &output); err == nil || !strings.Contains(err.Error(), "-trust") {
		t.Fatalf("缺少 TLS/信任配置必须在启动前失败: %v", err)
	}
}
