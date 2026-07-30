package versioningv1_test

import (
	"testing"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func FuzzVersionLedgerParsersNeverPanic(f *testing.F) {
	f.Add([]byte(`{"stream":{"namespace":"portal.configuration","streamId":"portal-main"},"idempotencyKey":"portal-main:revision:0001","content":{"layout":"standard"}}`))
	f.Add([]byte(`{"stream":{},"stream":null}`))
	f.Add([]byte(`not-json`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = versioningv1.ParseRequest(versioningv1.OperationPutVersion, raw)
		_, _ = versioningv1.ParseProviderRequest(versioningv1.ProviderOperationPutVersion, raw)
	})
}
