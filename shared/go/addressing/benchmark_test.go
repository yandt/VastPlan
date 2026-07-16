package addressing

import (
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/protocollimit"
	"context"
	"testing"
)

func BenchmarkBackend_AddressingLocalInvoke(b *testing.B) {
	r := &Router{Limits: protocollimit.Default(), local: map[string][]localHandler{}, localCursor: map[string]uint64{}}
	r.local["demo.echo"] = []localHandler{{handler: func(_ context.Context, _ *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, nil
	}}}
	target := &contractv1.CallTarget{Capability: "demo.echo"}
	callCtx := &contractv1.CallContext{TenantId: "acme"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := r.Invoke(context.Background(), target, callCtx, nil); err != nil {
			b.Fatal(err)
		}
	}
}
