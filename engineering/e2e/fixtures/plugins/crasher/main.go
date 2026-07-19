// 夹具插件：接入正常，但被调用 crash 操作时**直接杀死自己**（不走优雅退出）。
//
// 用于验证 ADR-0004 故障隔离：插件崩溃后宿主须感知断连、摘除其贡献、
// 并让在途调用立刻脱身（而非挂到超时）。
//
// 这是纯测试夹具，故放 engineering/e2e/fixtures/ 而非 extensions/plugins/（ADR-0018 §3）。
package main

import (
	"context"
	"log"
	"syscall"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	p := sdk.New("cn.vastplan.fixture.crasher", "0.1.0", map[string]string{"backend": "^0.1"})

	p.Contribute(sdk.Contribution{
		ExtensionPoint: "tool.package",
		ID:             "fixture.crasher",
		Descriptor:     []byte(`{"title":"故意崩溃的夹具"}`),
		Handlers: map[string]sdk.Handler{
			"ping":  ping,
			"crash": crash,
			"slow":  slow,
		},
	})

	if err := p.Serve(); err != nil {
		log.Fatalf("夹具插件退出: %v", err)
	}
}

func slow(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	select {
	case <-time.After(400 * time.Millisecond):
		return sdk.OK(400), []byte(`{"done":true}`), nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func ping(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	return sdk.OK(0), []byte(`{"pong":true}`), nil
}

// crash 用 SIGKILL 自杀：模拟真实崩溃——不发 SHUTDOWN、不关流、不回响应。
// 宿主只能靠"流断开"感知，这正是要验证的路径。
func crash(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	go func() {
		time.Sleep(50 * time.Millisecond) // 让宿主先把本次调用发出去，确保存在"在途调用"
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGKILL)
	}()
	select {} // 永不返回：调用方只能因插件死亡而脱身
}
