package credentialmaterial

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/runtimeaudience"
	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
)

type failingHost struct {
	result *contractv1.CallResult
	err    error
}

func (h failingHost) Call(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
	return h.result, nil, h.err
}

func testSource(t *testing.T, host failingHost) *Source {
	t.Helper()
	ref := commonv1.ManagedCredentialRef{
		Handle: "credential://managed/database", Scope: "tenant", Owner: "plugin.database", Purpose: "database.connection", Version: 1,
	}
	digest := sha256.Sum256([]byte("runtime"))
	source, err := New(host, "tenant-a", ref, runtimeaudience.FromDigest(digest))
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestSourcePreservesStableMaterialFailureWithoutRemoteDetail(t *testing.T) {
	source := testSource(t, failingHost{result: &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{
		Code: credentiallease.ErrorMaterialUnavailable, Message: "vault-address-and-secret", Retryable: false,
	}}})
	err := source.WithMaterial(context.Background(), time.Now().UTC(), func(Material) error { return nil })
	code, retryable, ok := credentiallease.FailureDetails(err)
	if !ok || code != credentiallease.ErrorMaterialUnavailable || retryable || err.Error() != credentiallease.SafeFailureMessage(code) {
		t.Fatalf("SDK 未保留稳定错误或泄漏了远端细节: code=%q retryable=%v ok=%v err=%v", code, retryable, ok, err)
	}
}

func TestSourceClassifiesHostTransportFailure(t *testing.T) {
	source := testSource(t, failingHost{err: errors.New("transport unavailable")})
	err := source.WithMaterial(context.Background(), time.Now().UTC(), func(Material) error { return nil })
	code, retryable, ok := credentiallease.FailureDetails(err)
	if !ok || code != credentiallease.ErrorServiceUnavailable || !retryable {
		t.Fatalf("宿主传输错误必须映射为可重试服务故障: code=%q retryable=%v ok=%v", code, retryable, ok)
	}
}
