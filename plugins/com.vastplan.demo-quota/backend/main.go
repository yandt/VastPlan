// 示例配额插件的 backend 面。
//
// 演示 **hook 分发**（§4.2/§4.3）的两个阶段：
//
//	demo.quota.limit (before, priority 100)：超配额 → 否决调用（一票否决）
//	demo.quota.meter (after,  priority  50)：记录调用计量（只观察，不改变结论）
//
// 限流与计量同属"配额管理"这一个职责，故合于一个插件（ADR-0020 高内聚）；
// 它们是插件而非内核内置——横切关注点也归插件（ADR-0001）。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
)

// quotaLimit 每个能力允许的调用次数上限。演示用的小值，便于观察否决行为。
const quotaLimit = 3

// quota 配额账本。演示用的内存实现——真实插件会落存储并按租户维度隔离。
type quota struct {
	mu sync.Mutex
	// calls 已放行的调用次数（由 before 钩子累加，是限流依据）
	calls map[string]int
	// metered after 钩子观察到的结论计数
	metered []meterRecord
}

type meterRecord struct {
	Capability string `json:"capability"`
	Operation  string `json:"operation,omitempty"`
	Status     string `json:"status"`
	DurationMs int64  `json:"durationMs"`
}

func main() {
	q := &quota{calls: map[string]int{}}
	p := sdk.New("com.vastplan.demo-quota", "0.1.0", map[string]string{"backend": "^0.1 || ^1.0"})

	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.Hook,
		ID:             "demo.quota.limit",
		Priority:       100,
		Descriptor: mustJSON(extpoint.HookDescriptor{
			Title: "配额限流", Point: extpoint.PointInvoke, Phase: extpoint.PhaseBefore,
		}),
		Handlers: map[string]sdk.Handler{"run": q.limit},
	})

	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.Hook,
		ID:             "demo.quota.meter",
		Priority:       50,
		Descriptor: mustJSON(extpoint.HookDescriptor{
			Title: "配额计量", Point: extpoint.PointInvoke, Phase: extpoint.PhaseAfter,
		}),
		Handlers: map[string]sdk.Handler{"run": q.meter},
	})

	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage,
		ID:             "demo.quota",
		Descriptor:     []byte(`{"title":"配额查询","subcommands":[{"name":"usage","description":"查询计量"}]}`),
		Handlers:       map[string]sdk.Handler{"usage": q.usage},
	})

	if err := p.Serve(); err != nil {
		log.Fatalf("插件退出: %v", err)
	}
}

// limit before 钩子：超过配额即否决。演示"一票否决"——它无需权限校验器配合，
// 限流与授权是两件事（授权说"你能不能"，配额说"你还剩多少"）。
func (q *quota) limit(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	var req extpoint.HookRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, nil, err
	}

	// 不限流配额查询自身，否则查不到"为什么被限流"
	if req.Target.Capability == "demo.quota" {
		return respond(extpoint.HookResponse{})
	}

	q.mu.Lock()
	q.calls[req.Target.Capability]++
	n := q.calls[req.Target.Capability]
	q.mu.Unlock()

	if n > quotaLimit {
		return respond(extpoint.HookResponse{
			Abort:  true,
			Reason: fmt.Sprintf("能力 %s 超出配额（上限 %d 次，本次第 %d 次）", req.Target.Capability, quotaLimit, n),
		})
	}
	return respond(extpoint.HookResponse{}) // 放行
}

// meter after 钩子：记录调用结论。只观察——返回什么都不会改变调用结果。
func (q *quota) meter(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	var req extpoint.HookRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, nil, err
	}
	if req.Result == nil {
		return nil, nil, fmt.Errorf("after 钩子应收到调用结论，实际为空")
	}

	q.mu.Lock()
	q.metered = append(q.metered, meterRecord{
		Capability: req.Target.Capability,
		Operation:  req.Target.Operation,
		Status:     req.Result.Status,
		DurationMs: req.Result.DurationMs,
	})
	q.mu.Unlock()

	return respond(extpoint.HookResponse{})
}

// usage 供外部验证钩子是否真的被执行。
func (q *quota) usage(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	out, err := json.Marshal(map[string]any{"metered": q.metered})
	if err != nil {
		return nil, nil, err
	}
	return sdk.OK(0), out, nil
}

func respond(r extpoint.HookResponse) (*contractv1.CallResult, []byte, error) {
	out, err := json.Marshal(r)
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
