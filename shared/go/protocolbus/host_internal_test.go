package protocolbus

// 同包测试：验证包内私有逻辑的边界。
//
// 这些函数（readAddr / newSessionID）不导出，只有同包 _test.go 能测——
// 正是"单元测试与源码同目录"的理由（ADR-0018 §1）。

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// testTimeout 单测用的短超时：超时值作参数注入，故无需真等 10s。
const testTimeout = 50 * time.Millisecond

// readAddr 解析插件经 stdout 回报的监听地址。
func TestReadAddr(t *testing.T) {
	cases := []struct {
		name    string
		stdout  string
		want    string
		wantErr bool
	}{
		{
			name:   "正常回报",
			stdout: "VASTPLAN_PLUGIN_ADDR|127.0.0.1:50051\n",
			want:   "127.0.0.1:50051",
		},
		{
			name:   "地址行前有其他输出——应跳过噪声继续找",
			stdout: "some log line\nanother line\nVASTPLAN_PLUGIN_ADDR|127.0.0.1:6000\n",
			want:   "127.0.0.1:6000",
		},
		{
			name:   "行首尾有空白——应被裁剪",
			stdout: "  VASTPLAN_PLUGIN_ADDR|127.0.0.1:7000  \n",
			want:   "127.0.0.1:7000",
		},
		{
			name:    "stdout 结束但从未回报地址——插件启动失败的典型表现",
			stdout:  "panic: something went wrong\n",
			wantErr: true,
		},
		{
			name:    "完全空输出",
			stdout:  "",
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := readAddr(strings.NewReader(c.stdout), testTimeout)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际得到地址 %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望解析出 %q，实际报错: %v", c.want, err)
			}
			if got != c.want {
				t.Fatalf("地址 = %q，期望 %q", got, c.want)
			}
		})
	}
}

// blockingReader 模拟"插件卡住、既不回报地址也不退出"。
type blockingReader struct{ release chan struct{} }

func (b *blockingReader) Read(p []byte) (int, error) {
	<-b.release // 一直阻塞，直到测试放行
	return 0, io.EOF
}

// 插件卡住时 readAddr 必须超时返回，而非永久挂起——
// 否则宿主会被一个坏插件拖死（ADR-0004 故障隔离的前提）。
func TestReadAddr_TimesOutWhenPluginHangs(t *testing.T) {
	r := &blockingReader{release: make(chan struct{})}
	defer close(r.release)

	start := time.Now()
	_, err := readAddr(r, testTimeout)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("插件卡住时应超时报错，实际返回成功")
	}
	if elapsed < testTimeout {
		t.Fatalf("应等满 %v 才超时，实际仅 %v（超时逻辑可能失效）", testTimeout, elapsed)
	}
	if elapsed > time.Second {
		t.Fatalf("超时应约 %v，实际耗时 %v（超时值可能没生效）", testTimeout, elapsed)
	}
}

// 生产用的超时值必须是个合理的正数——防止有人不小心改成 0（=立即超时，插件永远装不上）。
func TestAddrReportTimeout_Sane(t *testing.T) {
	if addrReportTimeout <= 0 {
		t.Fatalf("addrReportTimeout = %v，必须为正数", addrReportTimeout)
	}
	if addrReportTimeout < time.Second {
		t.Fatalf("addrReportTimeout = %v，过短会误杀启动慢的插件", addrReportTimeout)
	}
}

// 会话票据必须唯一——它是审计与插件回调鉴权的锚，重复即失去意义。
func TestNewSessionID_Unique(t *testing.T) {
	const n = 100
	seen := make(map[string]struct{}, n)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := newSessionID()
			mu.Lock()
			defer mu.Unlock()
			if _, dup := seen[id]; dup {
				t.Errorf("会话票据重复: %s", id)
			}
			seen[id] = struct{}{}
		}()
	}
	wg.Wait()

	if len(seen) != n {
		t.Fatalf("期望 %d 个唯一票据，实际 %d", n, len(seen))
	}
	for id := range seen {
		if !strings.HasPrefix(id, "sess-") {
			t.Fatalf("票据应带 sess- 前缀，实际 %q", id)
			break
		}
	}
}
