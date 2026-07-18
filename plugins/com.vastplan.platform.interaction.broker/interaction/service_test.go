package interaction

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	uiv1 "cdsoft.com.cn/VastPlan/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/shared/go/interactionapi"
)

func TestService_CompetesForOneTerminalResponseAndPersists(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	service, err := New(t.TempDir() + "/interactions.json")
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	source := interactionapi.Subject{ID: "com.vastplan.runner.workflow", TenantID: "tenant-a"}
	alice := interactionapi.Subject{ID: "alice", TenantID: "tenant-a", Roles: []string{"approver"}}
	bob := interactionapi.Subject{ID: "bob", TenantID: "tenant-a", Roles: []string{"approver"}}
	request := testRequest(now)
	if _, err := service.Open(context.Background(), source, request); err != nil {
		t.Fatalf("创建交互失败: %v", err)
	}
	listed, err := service.List(context.Background(), alice, uiv1.SurfaceFrontend)
	if err != nil || len(listed) != 1 {
		t.Fatalf("授权呈现端应看到一条待处理任务, records=%d err=%v", len(listed), err)
	}

	successes := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for _, subject := range []interactionapi.Subject{alice, bob} {
		subject := subject
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Respond(context.Background(), subject, request.ID, uiv1.SurfaceFrontend, uiv1.InteractionResponse{InteractionID: request.ID, Decision: uiv1.DecisionAnswered, Values: map[string]any{"reason": subject.ID}})
			if err == nil {
				successes <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(successes)
	if count := len(successes); count != 1 {
		t.Fatalf("并发响应只能有一个成功，实际 %d", count)
	}

	restarted, err := New(service.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return now }
	record, err := restarted.Get(context.Background(), source, request.ID)
	if err != nil || !record.State.Terminal() || record.Response == nil {
		t.Fatalf("重启后必须恢复终态响应，record=%+v err=%v", record, err)
	}
}

func TestService_RejectsCrossTenantAndSecretPlaintext(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	service, err := New(t.TempDir() + "/interactions.json")
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	source := interactionapi.Subject{ID: "com.vastplan.runner.workflow", TenantID: "tenant-a"}
	alice := interactionapi.Subject{ID: "alice", TenantID: "tenant-a", Roles: []string{"approver"}}
	request := testRequest(now)
	if _, err := service.Open(context.Background(), source, request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(context.Background(), interactionapi.Subject{ID: "mallory", TenantID: "tenant-b"}, request.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户读取必须不可见，err=%v", err)
	}
	_, err = service.Respond(context.Background(), alice, request.ID, uiv1.SurfaceFrontend, uiv1.InteractionResponse{
		InteractionID: request.ID,
		Decision:      uiv1.DecisionAnswered,
		Values:        map[string]any{"password": "plaintext"},
	})
	if err == nil {
		t.Fatal("秘密字段明文必须被拒绝")
	}
	record, err := service.Respond(context.Background(), alice, request.ID, uiv1.SurfaceFrontend, uiv1.InteractionResponse{
		InteractionID: request.ID,
		Decision:      uiv1.DecisionAnswered,
		CredentialRef: map[string]string{"password": "credential://tenant-a/temporary/123"},
	})
	if err != nil || record.Response == nil || record.Response.CredentialRef["password"] == "" {
		t.Fatalf("凭证引用应可作为终态响应，record=%+v err=%v", record, err)
	}
}

func TestService_ExpiresFailClosed(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	service, err := New(t.TempDir() + "/interactions.json")
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	source := interactionapi.Subject{ID: "com.vastplan.runner.workflow", TenantID: "tenant-a"}
	alice := interactionapi.Subject{ID: "alice", TenantID: "tenant-a", Roles: []string{"approver"}}
	request := testRequest(now)
	if _, err := service.Open(context.Background(), source, request); err != nil {
		t.Fatal(err)
	}
	now = request.ExpiresAt
	if _, err := service.Respond(context.Background(), alice, request.ID, uiv1.SurfaceFrontend, uiv1.InteractionResponse{InteractionID: request.ID, Decision: uiv1.DecisionAnswered}); !errors.Is(err, ErrExpired) {
		t.Fatalf("过期交互必须 fail-closed，err=%v", err)
	}
	restarted, err := New(service.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return now }
	record, err := restarted.Get(context.Background(), source, request.ID)
	if err != nil || record.State != interactionapi.StateExpired {
		t.Fatalf("过期交互必须持久化为 expired，record=%+v err=%v", record, err)
	}
}

func TestService_WatchResumesFromCursorAfterRendererResponse(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	service, err := New(t.TempDir() + "/interactions.json")
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	source := interactionapi.Subject{ID: "com.vastplan.runner.workflow", TenantID: "tenant-a"}
	alice := interactionapi.Subject{ID: "alice", TenantID: "tenant-a", Roles: []string{"approver"}}
	created, err := service.Open(context.Background(), source, testRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	watchContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan interactionapi.Record, 1)
	errs := make(chan error, 1)
	go func() {
		record, err := service.Watch(watchContext, source, created.Request.ID, created.UpdatedAt)
		if err != nil {
			errs <- err
			return
		}
		result <- record
	}()
	if _, err := service.Respond(context.Background(), alice, created.Request.ID, uiv1.SurfaceFrontend, uiv1.InteractionResponse{InteractionID: created.Request.ID, Decision: uiv1.DecisionAnswered, CredentialRef: map[string]string{"password": "credential://tenant-a/temporary/123"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errs:
		t.Fatalf("watch 不应失败: %v", err)
	case record := <-result:
		if record.State != interactionapi.StateAnswered || record.Response == nil {
			t.Fatalf("watch 必须返回终态记录: %+v", record)
		}
	case <-time.After(time.Second):
		t.Fatal("watch 未在响应后恢复")
	}

	// 断线重连使用旧 cursor 时，持久化终态必须立即返回而无需重新创建任务。
	restarted, err := New(service.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return now }
	record, err := restarted.Watch(context.Background(), source, created.Request.ID, created.UpdatedAt)
	if err != nil || record.State != interactionapi.StateAnswered {
		t.Fatalf("重连 watch 必须恢复已持久化终态: record=%+v err=%v", record, err)
	}
}

func testRequest(now time.Time) uiv1.InteractionRequest {
	return uiv1.InteractionRequest{
		ID:              "interaction-0001",
		ContractVersion: uiv1.InteractionContractVersion,
		Kind:            uiv1.InteractionForm,
		Source:          uiv1.InteractionSource{Capability: "com.vastplan.runner.workflow", Operation: "run"},
		TenantID:        "tenant-a",
		EligibleSubjects: []string{
			"role:approver",
		},
		AllowedSurfaces: []uiv1.InteractionSurface{uiv1.SurfaceFrontend, uiv1.SurfaceMobile},
		ExpiresAt:       now.Add(time.Hour),
		Form: &uiv1.FormSchema{ID: "approval", Fields: []uiv1.FormField{
			{Key: "reason", Type: uiv1.FieldText, Title: "Reason"},
			{Key: "password", Type: uiv1.FieldSecretRef, Title: "Credential"},
		}},
	}
}
