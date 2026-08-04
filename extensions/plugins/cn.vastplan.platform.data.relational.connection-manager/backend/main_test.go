package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func TestDescriptorMatchesSignedManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var signed, runtime any
	if len(contributions) != 1 || json.Unmarshal(contributions[0].Descriptor, &signed) != nil || json.Unmarshal(descriptor(), &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
		t.Fatalf("运行时 descriptor 与签名 Manifest 不一致\nsigned=%s\nruntime=%s", contributions[0].Descriptor, descriptor())
	}
}

type platformControlHost struct {
	target  *contractv1.CallTarget
	payload []byte
}

func (h *platformControlHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.target = target
	h.payload = append([]byte(nil), payload...)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"phase":"unconfigured"}`), nil
}

func TestPlatformControlOperationsForwardOnlyValidatedRequestsToKernelServices(t *testing.T) {
	host := &platformControlHost{}
	request := []byte(`{"profile":{"schemaVersion":1,"generation":1,"providerId":"postgresql","endpoint":"db:5432","database":"vastplan","schema":"platform","tls":{"mode":"verify-full","serverName":"db"},"username":"app","secretRef":{"kind":"systemd-credential","name":"platform-db"},"contractRange":"^1.0.0"},"expectedGeneration":0}`)
	result, _, err := callPlatformControl(context.Background(), host, dbContext(), operationPlatformControlTest, request)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || host.target.GetExtensionPoint() != extpoint.KernelService || host.target.GetCapability() != platformcontrolv1.KernelTestService || host.target.GetOperation() != "test" {
		t.Fatalf("Platform Control 测试未通过受限内核端口转发: target=%+v result=%+v err=%v", host.target, result, err)
	}
	result, _, err = callPlatformControl(context.Background(), host, dbContext(), operationPlatformControlConfigure, []byte(`{"profile":{"schemaVersion":1,"generation":8},"expectedGeneration":0}`))
	if err != nil || result.GetError().GetCode() != "platform.database.platform_control_invalid" {
		t.Fatalf("非法配置必须在插件边界拒绝: result=%+v err=%v", result, err)
	}
}

type sharedStateProbeHost struct {
	probeHost
	values    map[string][]byte
	revisions map[string]uint64
}

func newSharedStateProbeHost() *sharedStateProbeHost {
	return &sharedStateProbeHost{values: map[string][]byte{}, revisions: map[string]uint64{}}
}

func (h *sharedStateProbeHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	operation := ""
	switch target.GetCapability() {
	case sharedstatev1.KernelService(sharedstatev1.OperationGet):
		operation = sharedstatev1.OperationGet
	case sharedstatev1.FencedKernelService(sharedstatev1.OperationCreate):
		operation = sharedstatev1.OperationCreate
	case sharedstatev1.FencedKernelService(sharedstatev1.OperationUpdate):
		operation = sharedstatev1.OperationUpdate
	default:
		return h.probeHost.Call(ctx, target, call, payload)
	}
	key := call.GetTenantId()
	if operation == sharedstatev1.OperationGet {
		if h.revisions[key] == 0 {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "state.not_found", Message: "not found"}}, nil, nil
		}
		return sharedStateEntryResult(h.values[key], h.revisions[key])
	}
	parsed, err := sharedstatev1.ParseRequest(operation, payload)
	if err != nil {
		return nil, nil, err
	}
	request := parsed.(*sharedstatev1.WriteRequest)
	value, err := sharedstatev1.DecodeValue(request.Value)
	if err != nil {
		return nil, nil, err
	}
	if operation == sharedstatev1.OperationCreate && h.revisions[key] != 0 || operation == sharedstatev1.OperationUpdate && request.ExpectedRevision != h.revisions[key] {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "state.conflict", Message: "conflict", Retryable: true}}, nil, nil
	}
	h.revisions[key]++
	h.values[key] = append([]byte(nil), value...)
	return sharedStateEntryResult(value, h.revisions[key])
}

func sharedStateEntryResult(value []byte, revision uint64) (*contractv1.CallResult, []byte, error) {
	raw, err := json.Marshal(sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: sharedStateKey, Value: sharedstatev1.EncodeValue(value), Revision: revision, UpdatedAt: time.Now().UTC()})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
}

func TestSharedStateProtocolPersistsTenantSagaAcrossPluginRestart(t *testing.T) {
	host := newSharedStateProbeHost()
	service := newSharedStateService()
	payload := []byte(`{"name":"primary","providerId":"postgresql","endpoint":"db.internal:5432","options":{"user":"app"},"credentialValue":"one-time-password"}`)
	result, raw, err := service.handle(context.Background(), host, dbContext(), payload, "define")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !strings.Contains(string(raw), `"name":"primary"`) {
		t.Fatalf("Shared State 定义连接失败: result=%+v raw=%s err=%v", result, raw, err)
	}
	if host.revisions["tenant-a"] < 2 || strings.Contains(string(host.values["tenant-a"]), "one-time-password") {
		t.Fatalf("Shared State 未形成 CAS 工作流或泄漏密码: revision=%d state=%s", host.revisions["tenant-a"], host.values["tenant-a"])
	}
	restarted := newSharedStateService()
	result, raw, err = restarted.handle(context.Background(), host, dbContext(), []byte(`{}`), "list")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !strings.Contains(string(raw), `"name":"primary"`) {
		t.Fatalf("插件重启后未从 Shared State 恢复连接: result=%+v raw=%s err=%v", result, raw, err)
	}
	other := dbContext()
	other.TenantId = "tenant-b"
	_, raw, err = restarted.handle(context.Background(), host, other, []byte(`{}`), "list")
	if err != nil || string(raw) != "[]" {
		t.Fatalf("租户状态未隔离: raw=%s err=%v", raw, err)
	}
}

type probeHost struct {
	payload          []byte
	activated        int
	runtimeActivated int
	retired          int
	failRetire       int
	failActivations  int
	failRuntime      int
	runtimeErrorCode string
}

var _ sdk.Host = (*probeHost)(nil)

func (h *probeHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.GetCapability() == credentialCapability {
		switch target.GetOperation() {
		case "stageManaged":
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"stageId":"stage-1","ref":{"handle":"credential://managed/opaque-1","scope":"tenant","owner":"cn.vastplan.platform.data.relational.connection-manager","purpose":"database.connection","version":1}}`), nil
		case "activateManaged":
			if h.failActivations > 0 {
				h.failActivations--
				return nil, nil, errors.New("credential service temporarily unavailable")
			}
			h.activated++
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		case "retireManaged":
			if h.failRetire > 0 {
				h.failRetire--
				return nil, nil, errors.New("credential retirement temporarily unavailable")
			}
			h.retired++
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		case "abortManaged":
			if h.activated > h.retired {
				return nil, nil, errors.New("active credential cannot be aborted")
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		}
	}
	if target.GetCapability() == databasev1.Capability {
		h.payload = append([]byte(nil), payload...)
		switch target.GetOperation() {
		case databasev1.OperationActivate:
			if h.failRuntime > 0 {
				h.failRuntime--
				return nil, nil, errors.New("runtime temporarily unavailable")
			}
			h.runtimeActivated++
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"connection":{"resourceId":"connection-test","revision":1},"generation":1,"ready":true}`), nil
		case databasev1.OperationProbe:
			if h.runtimeErrorCode != "" {
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: h.runtimeErrorCode, Message: "internal endpoint=db.internal password=do-not-leak", Retryable: true}}, nil, nil
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"ready":true,"providerId":"postgresql","latencyMs":1}`), nil
		case databasev1.OperationRetire:
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		}
	}
	return nil, nil, errors.New("unexpected capability or operation")
}

func TestCurrentFormConnectionTestMapsRuntimeFailureWithoutLeakingProviderDetail(t *testing.T) {
	service, err := newService(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{runtimeErrorCode: databasev1.ErrorConnectionUnavailable}
	result, raw, err := service.handle(context.Background(), host, dbContext(), []byte(`{"name":"draft","providerId":"postgresql","endpoint":"db.internal:5432","options":{"user":"app"},"credentialValue":"one-time-password"}`), "test")
	if err != nil || result.GetError().GetCode() != "platform.database.connection_unavailable" || result.GetError().GetMessage() != "数据库连接暂时不可用" || len(raw) != 0 {
		t.Fatalf("Runtime 故障必须转换成安全平台错误: result=%+v raw=%s err=%v", result, raw, err)
	}
	if strings.Contains(result.GetError().GetMessage(), "db.internal") || strings.Contains(result.GetError().GetMessage(), "do-not-leak") {
		t.Fatalf("Portal 可见错误不得包含 Provider 诊断: %q", result.GetError().GetMessage())
	}
}

func TestCurrentFormConnectionTestPreservesSafeRuntimeDiagnosis(t *testing.T) {
	for _, test := range []struct {
		runtimeCode  string
		platformCode string
		message      string
	}{
		{databasev1.ErrorTLSPolicyForbidden, "platform.database.tls_policy_forbidden", "当前部署策略不允许关闭数据库传输加密校验"},
		{databasev1.ErrorNameResolutionFailed, "platform.database.name_resolution_failed", "数据库地址无法解析"},
		{databasev1.ErrorConnectionRefused, "platform.database.connection_refused", "数据库服务器拒绝了连接"},
		{databasev1.ErrorConnectionTimeout, "platform.database.connection_timeout", "连接数据库超时"},
		{databasev1.ErrorTLSVerificationFailed, "platform.database.tls_verification_failed", "数据库传输加密或证书校验失败"},
		{databasev1.ErrorAuthenticationFailed, "platform.database.authentication_failed", "数据库用户名或密码验证失败"},
		{databasev1.ErrorDatabaseNotFound, "platform.database.database_not_found", "指定的数据库不存在"},
		{databasev1.ErrorPermissionDenied, "platform.database.permission_denied", "数据库账户没有所需权限"},
		{databasev1.ErrorCredentialUnavailable, "platform.database.credential_unavailable", "数据库密码不可用或已经失效，请重新输入密码"},
		{databasev1.ErrorCredentialServiceUnavailable, "platform.database.credential_service_unavailable", "数据库凭证服务暂时不可用，请稍后重试"},
	} {
		t.Run(test.runtimeCode, func(t *testing.T) {
			service, err := newService(filepath.Join(t.TempDir(), "connections.json"))
			if err != nil {
				t.Fatal(err)
			}
			result, raw, err := service.handle(context.Background(), &probeHost{runtimeErrorCode: test.runtimeCode}, dbContext(), []byte(`{"name":"draft","providerId":"postgresql","endpoint":"db.internal:5432","options":{"user":"app"},"credentialValue":"one-time-password"}`), "test")
			if err != nil || len(raw) != 0 || result.GetError().GetCode() != test.platformCode || result.GetError().GetMessage() != test.message {
				t.Fatalf("诊断映射错误: result=%+v raw=%s err=%v", result, raw, err)
			}
			if strings.Contains(result.GetError().GetMessage(), "db.internal") || strings.Contains(result.GetError().GetMessage(), "do-not-leak") {
				t.Fatalf("安全诊断泄露 Provider 原文: %q", result.GetError().GetMessage())
			}
		})
	}
}

func TestSavedConnectionProbeUsesTheSameCredentialDiagnosis(t *testing.T) {
	service, err := newService(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{}
	if _, _, err := service.handle(context.Background(), host, dbContext(), []byte(`{"name":"saved","providerId":"postgresql","endpoint":"db.internal:5432","options":{"user":"app"},"credentialValue":"one-time-password"}`), "define"); err != nil {
		t.Fatal(err)
	}
	host.runtimeErrorCode = databasev1.ErrorCredentialUnavailable
	result, raw, err := service.handle(context.Background(), host, dbContext(), []byte(`{"name":"saved"}`), "probe")
	if err != nil || len(raw) != 0 || result.GetError().GetCode() != "platform.database.credential_unavailable" || result.GetError().GetMessage() != "数据库密码不可用或已经失效，请重新输入密码" {
		t.Fatalf("列表 probe 与编辑 test 的凭证诊断不一致: result=%+v raw=%s err=%v", result, raw, err)
	}
}

func TestInterruptedConnectionTestRecoversTemporaryCredentialRetirement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	s, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{failRetire: 2}
	payload := []byte(`{"name":"draft","providerId":"postgresql","endpoint":"db.internal:5432","options":{"user":"app"},"credentialValue":"one-time-password"}`)
	if _, _, err := s.handle(context.Background(), host, dbContext(), payload, "test"); err == nil {
		t.Fatal("临时凭证无法退役时测试必须失败并保留回收任务")
	}
	if len(s.data.TestCleanup["tenant-a"]) != 1 {
		t.Fatalf("临时凭证回收任务未耐久保留: %+v", s.data.TestCleanup)
	}
	reopened, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.handle(context.Background(), host, dbContext(), []byte(`{}`), "list"); err != nil {
		t.Fatal(err)
	}
	if len(reopened.data.TestCleanup["tenant-a"]) != 0 || host.retired != 1 {
		t.Fatalf("重启后未收敛临时凭证回收: queue=%+v retired=%d", reopened.data.TestCleanup, host.retired)
	}
}

func TestCurrentFormConnectionTestUsesTemporaryCredentialWithoutSavingDefinition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	s, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{}
	payload := []byte(`{"name":"draft","providerId":"postgresql","endpoint":"db.internal:5432","database":"app","options":{"user":"app","tlsMode":"verify-full"},"credentialValue":"one-time-password"}`)
	_, raw, err := s.handle(context.Background(), host, dbContext(), payload, "test")
	if err != nil || !strings.Contains(string(raw), `"ready":true`) || !strings.Contains(string(raw), `"latencyMs":1`) {
		t.Fatalf("当前表单连接测试失败: raw=%s err=%v", raw, err)
	}
	if host.activated != 1 || host.retired != 1 {
		t.Fatalf("测试凭证没有完成短期激活和退役: activated=%d retired=%d", host.activated, host.retired)
	}
	if len(s.data.Tenants["tenant-a"]) != 0 || len(s.data.Revisions["tenant-a"]) != 0 || len(s.data.Publications["tenant-a"]) != 0 || len(s.data.Retire["tenant-a"]) != 0 || len(s.data.TestCleanup["tenant-a"]) != 0 {
		t.Fatalf("连接测试污染了正式管理状态: %+v", s.data)
	}
	state, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(state), "one-time-password") || strings.Contains(string(state), "db.internal") {
		t.Fatalf("测试状态泄露秘密或未保存连接定义: %s", state)
	}
}

func TestRuntimePublicationOutboxRecoversWithoutLosingDefinition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	service, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{failRuntime: 1}
	_, raw, err := service.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary","providerId":"postgresql","endpoint":"db.internal:5432","options":{"user":"app"},"credentialValue":"correct-horse"}`), "define")
	if err != nil || !strings.Contains(string(raw), `"runtime":"pending"`) {
		t.Fatalf("Runtime 暂不可用时应保存期望定义并报告 pending: raw=%s err=%v", raw, err)
	}
	reopened, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err = reopened.handle(context.Background(), host, dbContext(), []byte(`{}`), "list")
	if err != nil || !strings.Contains(string(raw), `"runtime":"ready"`) || host.runtimeActivated != 1 {
		t.Fatalf("publication outbox 未在重启后收敛: raw=%s runtime=%d err=%v", raw, host.runtimeActivated, err)
	}
}

func TestDeleteAndRecreateKeepsConnectionRevisionMonotonic(t *testing.T) {
	service, err := newService(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{}
	define := []byte(`{"name":"primary","providerId":"postgresql","endpoint":"db.internal:5432","options":{"user":"app"},"credentialValue":"correct-horse"}`)
	if _, raw, err := service.handle(context.Background(), host, dbContext(), define, "define"); err != nil || !strings.Contains(string(raw), `"revision":1`) {
		t.Fatalf("首次定义失败: raw=%s err=%v", raw, err)
	}
	if _, _, err := service.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary"}`), "remove"); err != nil {
		t.Fatal(err)
	}
	if _, raw, err := service.handle(context.Background(), host, dbContext(), define, "define"); err != nil || !strings.Contains(string(raw), `"revision":2`) {
		t.Fatalf("同名重建不得回退 revision: raw=%s err=%v", raw, err)
	}
}

func TestPendingCredentialActivationRecoversAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	service, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{failActivations: 1}
	if _, _, err := service.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary","providerId":"postgresql","endpoint":"db.internal:5432","options":{"user":"app"},"credentialValue":"correct-horse"}`), "define"); err == nil {
		t.Fatal("第一次激活失败应返回错误并保留 pending")
	}
	reopened, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	result, raw, err := reopened.handle(context.Background(), host, dbContext(), []byte(`{}`), "list")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !strings.Contains(string(raw), `"credential":{"managed":true,"version":1}`) || strings.Contains(string(raw), "credential://managed/") || host.activated != 1 || host.runtimeActivated != 1 {
		t.Fatalf("重启后未收敛 pending: raw=%s activated=%d runtime=%d err=%v", raw, host.activated, host.runtimeActivated, err)
	}
}
func dbContext() *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "admin"}}
}
func TestConnectionDefinitionPersistsAndProbeHasNoSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	s, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	host := &probeHost{}
	_, response, err := s.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary","providerId":"postgresql","endpoint":"db.internal:5432","database":"app","options":{"user":"app"},"credentialValue":"correct-horse"}`), "define")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(response), "correct-horse") || strings.Contains(string(response), "credential://managed/") || host.activated != 1 || host.runtimeActivated != 1 {
		t.Fatalf("响应泄露明文或候选未激活: response=%s activated=%d", response, host.activated)
	}
	reopened, err := newService(path)
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := reopened.handle(context.Background(), host, dbContext(), []byte(`{"name":"primary"}`), "probe")
	if err != nil || !strings.Contains(string(raw), `"ready":true`) || !strings.Contains(string(raw), `"providerId":"postgresql"`) || !strings.Contains(string(raw), `"latencyMs":1`) {
		t.Fatalf("probe failed raw=%s err=%v", raw, err)
	}
	if strings.Contains(string(host.payload), "password") || strings.Contains(string(host.payload), "secret") {
		t.Fatalf("probe payload leaked secret: %s", host.payload)
	}
	var request map[string]any
	if err := json.Unmarshal(host.payload, &request); err != nil {
		t.Fatal(err)
	}
	connection, ok := request["connection"].(map[string]any)
	if !ok || connection["credentials"].(map[string]any)["handle"] != "credential://managed/opaque-1" {
		t.Fatalf("credential reference missing: %s", host.payload)
	}
}
