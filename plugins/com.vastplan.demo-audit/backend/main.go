// 示例审计插件的 backend 面。
//
// 演示 **fanout 分发**（§4.2）：订阅 `task.*` 与 `plugin.activated`，宿主把匹配的事件
// 扇出给所有订阅者。审计是插件而非内核内置——这正是"一切功能皆插件"（ADR-0001）。
//
// 它还贡献一个工具用于查询已记录的事件，使"扇出是否真的送达"可被外部验证。
package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
)

// ledger 已记录的事件。演示用的内存账本——真实审计插件会落存储。
type ledger struct {
	mu     sync.Mutex
	events []recorded
}

type recorded struct {
	Type    string `json:"type"`
	Source  string `json:"source"`
	Subject string `json:"subject,omitempty"`
}

func (l *ledger) add(r recorded) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, r)
}

func (l *ledger) snapshot() []recorded {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]recorded, len(l.events))
	copy(out, l.events)
	return out
}

func main() {
	book := &ledger{}
	p := sdk.New("com.vastplan.demo-audit", "0.1.0", map[string]string{"backend": "^0.1 || ^1.0"})

	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.EventSink,
		ID:             "demo.audit.sink",
		Priority:       50,
		Descriptor: mustJSON(extpoint.SinkDescriptor{
			Title: "审计事件汇",
			// 只订阅这些类型；未声明的事件宿主不会发来（fail-closed：没声明就别收）
			Subscribe: []string{"task.*", "plugin.activated"},
		}),
		Handlers: map[string]sdk.Handler{"consume": book.consume},
	})

	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage,
		ID:             "demo.audit",
		Descriptor:     []byte(`{"title":"审计查询","subcommands":[{"name":"list","description":"列出已记录的事件"}]}`),
		Handlers:       map[string]sdk.Handler{"list": book.list},
	})

	if err := p.Serve(); err != nil {
		log.Fatalf("插件退出: %v", err)
	}
}

// consume 收下宿主扇出的事件并记账。
func (l *ledger) consume(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	var ev struct {
		Type    string `json:"type"`
		Source  string `json:"source"`
		Subject string `json:"subject"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil, nil, err
	}
	l.add(recorded{Type: ev.Type, Source: ev.Source, Subject: ev.Subject})
	return sdk.OK(0), []byte(`{"recorded":true}`), nil
}

// list 供外部验证扇出是否真的送达。
func (l *ledger) list(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	out, err := json.Marshal(map[string]any{"events": l.snapshot()})
	if err != nil {
		return nil, nil, err
	}
	return sdk.OK(0), out, nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // 常量级 descriptor 序列化失败属编码错误，应在开发期立即暴露
	}
	return b
}
