package credentials

import (
	"errors"
	"testing"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
)

func TestInvalidVaultCiphertextBecomesNonRetryableMaterialFailure(t *testing.T) {
	err := classifyTransitLeaseFailure(&VaultTransitError{
		Operation: "decrypt", StatusCode: 400, InvalidMaterial: true,
	})
	code, retryable, ok := credentiallease.FailureDetails(err)
	if !ok || code != credentiallease.ErrorMaterialUnavailable || retryable {
		t.Fatalf("不可解密密文必须要求调用方更新凭证: code=%q retryable=%v ok=%v", code, retryable, ok)
	}
}

func TestVaultOutageBecomesRetryableServiceFailure(t *testing.T) {
	err := classifyTransitLeaseFailure(&VaultTransitError{
		Operation: "decrypt", Retryable: true, Err: errors.New("network unavailable"),
	})
	code, retryable, ok := credentiallease.FailureDetails(err)
	if !ok || code != credentiallease.ErrorServiceUnavailable || !retryable {
		t.Fatalf("Vault 暂时不可用必须保留重试语义: code=%q retryable=%v ok=%v", code, retryable, ok)
	}
}
