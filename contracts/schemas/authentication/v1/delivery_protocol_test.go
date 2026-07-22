package authenticationv1_test

import (
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
)

func TestDeliveryProtocolRejectsSecretsAndAmbiguousSubjects(t *testing.T) {
	request := authenticationv1.DeliveryRequest{ChallengeID: "challenge.12345678", DeliveryProfileID: "enterprise-mail", Channel: authenticationv1.DeliveryEmail, Identifier: "alice@example.com", Locale: "zh-CN", Code: "123456", ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}
	if _, err := authenticationv1.ParseDeliveryRequest(authenticationv1.OperationDeliver, marshal(t, request)); err != nil {
		t.Fatal(err)
	}
	leaking := append(marshal(t, request)[:len(marshal(t, request))-1], []byte(`,"token":"secret"}`)...)
	if _, err := authenticationv1.ParseDeliveryRequest(authenticationv1.OperationDeliver, leaking); err == nil {
		t.Fatal("Delivery 请求不得携带未定义 secret/token")
	}
	if _, err := authenticationv1.ParseDeliveryResult(authenticationv1.OperationDeliver, []byte(`{"accepted":false,"subjectId":"alice"}`)); err == nil {
		t.Fatal("未接受投递不得返回主体")
	}
	request.ExpiresAt = time.Now().UTC().Add(11 * time.Minute)
	if _, err := authenticationv1.ParseDeliveryRequest(authenticationv1.OperationDeliver, marshal(t, request)); err == nil {
		t.Fatal("Delivery 挑战不得超过最大时间窗")
	}
}
