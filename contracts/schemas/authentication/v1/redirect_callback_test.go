package authenticationv1_test

import (
	"strings"
	"testing"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
)

func TestRedirectContinueHasDedicatedBoundedCallback(t *testing.T) {
	request := authenticationv1.ContinueRequest{TransactionID: strings.Repeat("t", 32), StepID: strings.Repeat("s", 32), Redirect: &authenticationv1.RedirectResponse{Code: "authorization-code", State: strings.Repeat("x", 32)}}
	if _, err := authenticationv1.ParseMethodRequest(authenticationv1.OperationContinue, marshal(t, request)); err != nil {
		t.Fatal(err)
	}
	request.Responses = []authenticationv1.FieldResponse{{FieldID: "code", Value: "shadow"}}
	if _, err := authenticationv1.ParseMethodRequest(authenticationv1.OperationContinue, marshal(t, request)); err == nil {
		t.Fatal("redirect 与 responses 不得同时出现")
	}
	request.Responses = nil
	request.Redirect = &authenticationv1.RedirectResponse{Code: "code", Error: "access_denied", State: strings.Repeat("x", 32)}
	if _, err := authenticationv1.ParseMethodRequest(authenticationv1.OperationContinue, marshal(t, request)); err == nil {
		t.Fatal("redirect code 与 error 必须互斥")
	}
	object := map[string]any{"transactionId": strings.Repeat("t", 32), "stepId": strings.Repeat("s", 32), "redirect": map[string]any{"code": "code", "state": strings.Repeat("x", 32), "access_token": "forbidden"}}
	if _, err := authenticationv1.ParseMethodRequest(authenticationv1.OperationContinue, marshal(t, object)); err == nil {
		t.Fatal("redirect 回调不得携带 Token 或任意参数")
	}
}
