package protocolbus

import (
	"context"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocollimit"
)

func BenchmarkBackend_ProtocolDeadlineAndContextClone(b *testing.B) {
	callCtx := &contractv1.CallContext{TenantId: "acme", Metadata: map[string]string{"scene": "benchmark"}}
	limits := protocollimit.Default()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, cancel := boundedCallContext(context.Background(), callCtx, limits)
		cancel()
	}
}

func BenchmarkBackend_SessionCorrelation(b *testing.B) {
	s := newSession("s", "p", "1.0.0")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := s.nextRequestID()
		if _, err := s.await(id, 1); err != nil {
			b.Fatal(err)
		}
		s.release(id)
	}
}
