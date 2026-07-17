package protocolbus

import (
	"fmt"
	"strings"
	"testing"
)

func TestProcessLogWriterCapturesAndBoundsPluginOutput(t *testing.T) {
	var lines []string
	writer := processLogWriter{prefix: "plugin=test stream=stdout", logf: func(format string, values ...any) {
		lines = append(lines, fmt.Sprintf(format, values...))
	}}
	input := strings.Repeat("x", (64<<10)+100)
	if n, err := writer.Write([]byte(input)); err != nil || n != len(input) {
		t.Fatalf("写入失败 n=%d err=%v", n, err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "[truncated]") || !strings.Contains(lines[0], "plugin=test") {
		t.Fatalf("插件日志未接管或未截断: %q", lines)
	}
}
