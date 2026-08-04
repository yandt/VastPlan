package databaseruntime

import (
	"errors"
	"testing"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
)

func TestCredentialLeaseFailuresRemainDistinctFromDatabaseNetworkFailures(t *testing.T) {
	for _, test := range []struct {
		leaseCode string
		retryable bool
		wantCode  string
	}{
		{credentiallease.ErrorMaterialUnavailable, false, databasev1.ErrorCredentialUnavailable},
		{credentiallease.ErrorReferenceUnavailable, false, databasev1.ErrorCredentialUnavailable},
		{credentiallease.ErrorServiceUnavailable, true, databasev1.ErrorCredentialServiceUnavailable},
	} {
		leaseFailure := credentiallease.NewFailure(test.leaseCode, test.retryable, errors.New("trusted detail"))
		classified, ok := classifyCredentialLeaseError(leaseFailure)
		code, retryable := ErrorDetails(classified)
		if !ok || code != test.wantCode || retryable != test.retryable {
			t.Fatalf("凭证失败分类错误: lease=%s code=%s retryable=%v ok=%v", test.leaseCode, code, retryable, ok)
		}
		throughSQL := classifySQLError(leaseFailure, false)
		if sqlCode, sqlRetryable := ErrorDetails(throughSQL); sqlCode != test.wantCode || sqlRetryable != test.retryable {
			t.Fatalf("database/sql 边界丢失凭证分类: lease=%s code=%s retryable=%v", test.leaseCode, sqlCode, sqlRetryable)
		}
	}
}
