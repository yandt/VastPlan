package portalcomposer

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/schemas/composition/frontend/v1"
	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/portalapi"
)

type acceptingCatalog struct{}

func (acceptingCatalog) ValidatePortal(context.Context, string, portalapi.PortalSpec) error {
	return nil
}

func principal(id string, roles ...string) portalapi.Principal {
	return portalapi.Principal{ID: id, TenantID: "tenant-a", Roles: roles}
}
func spec(route string) frontendcompositionv1.ApplicationComposition {
	value := testComposition(route)
	value.Plugins = []frontendcompositionv1.PluginRef{}
	return value
}
func newTestService(t *testing.T) *Service {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "portals.json"), acceptingCatalog{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BindPlatformProfile(testProfile()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGovernedPublishRequiresDifferentApproverAndPersistsAudit(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose", "portal.approve")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	draft, err := s.CreateDraft(context.Background(), author, spec("/"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(context.Background(), author, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(context.Background(), author, draft.ID); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("自审必须被拒绝: %v", err)
	}
	if _, err := s.Approve(context.Background(), approver, draft.ID); err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish(context.Background(), publisher, draft.ID, portalapi.PublishRequest{})
	if err != nil || published.Status != portalapi.StatusPublished || !published.Active {
		t.Fatalf("发布失败: %+v %v", published, err)
	}
	audit, err := s.Audit(context.Background(), publisher, draft.ID)
	if err != nil || len(audit) != 4 {
		t.Fatalf("审计事件应完整保留: %+v %v", audit, err)
	}
	if reopened, err := New(filepath.Join(filepath.Dir(s.stateFile), "portals.json"), acceptingCatalog{}); err != nil {
		t.Fatal(err)
	} else if err := reopened.BindPlatformProfile(testProfile()); err != nil {
		t.Fatal(err)
	} else if got, err := reopened.List(context.Background(), publisher); err != nil || len(got) != 1 || got[0].Status != portalapi.StatusPublished {
		t.Fatalf("持久化状态错误: %+v %v", got, err)
	}
}

func TestPublishRejectsCrossPortalRouteAndBreakGlassNeedsReason(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	publish := func(id string) {
		d, e := s.CreateDraft(context.Background(), author, spec("/"))
		if e != nil {
			t.Fatal(e)
		}
		s.state.Revisions[len(s.state.Revisions)-1].PortalID = id
		if _, e = s.Submit(context.Background(), author, d.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = s.Approve(context.Background(), approver, d.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = s.Publish(context.Background(), publisher, d.ID, portalapi.PublishRequest{}); e != nil {
			t.Fatal(e)
		}
	}
	publish("one")
	d, err := s.CreateDraft(context.Background(), author, spec("/"))
	if err != nil {
		t.Fatal(err)
	}
	s.state.Revisions[len(s.state.Revisions)-1].PortalID = "two"
	if _, err = s.Publish(context.Background(), portalapi.Principal{ID: "system", TenantID: "tenant-a", System: true}, d.ID, portalapi.PublishRequest{}); err == nil {
		t.Fatal("break-glass 缺原因必须拒绝")
	}
	if _, err = s.Submit(context.Background(), author, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Approve(context.Background(), approver, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(context.Background(), publisher, d.ID, portalapi.PublishRequest{}); !errors.Is(err, ErrRouteConflict) {
		t.Fatalf("同租户跨 Portal 路由冲突必须拒绝: %v", err)
	}
}

func TestRollbackUsesInactivePublishedRevisionAndReturnsActiveCopy(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	publish := func() portalapi.Revision {
		t.Helper()
		draft, err := s.CreateDraft(context.Background(), author, spec("/"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Submit(context.Background(), author, draft.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Approve(context.Background(), approver, draft.ID); err != nil {
			t.Fatal(err)
		}
		published, err := s.Publish(context.Background(), publisher, draft.ID, portalapi.PublishRequest{})
		if err != nil {
			t.Fatal(err)
		}
		return published
	}
	first := publish()
	_ = publish()
	rolledBack, err := s.Rollback(context.Background(), publisher, first.ID, portalapi.PublishRequest{})
	if err != nil || !rolledBack.Active || rolledBack.Status != portalapi.StatusPublished {
		t.Fatalf("历史 revision 回滚失败: revision=%+v err=%v", rolledBack, err)
	}
	if _, err := s.Rollback(context.Background(), publisher, rolledBack.ID, portalapi.PublishRequest{}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("当前 active revision 不能作为回滚源: %v", err)
	}
}

type configuredHost struct {
	stateFile string
	calls     []string
}

var _ sdk.Host = (*configuredHost)(nil)

func (h *configuredHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.GetExtensionPoint() != extpoint.KernelService {
		return nil, nil, errors.New("unexpected extension point")
	}
	h.calls = append(h.calls, target.GetCapability())
	switch target.GetCapability() {
	case "kernel.config.get":
		var request map[string]string
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, nil, errors.New("unexpected state configuration request")
		}
		var value string
		switch request["key"] {
		case StateFileConfigKey:
			value = h.stateFile
		case PlatformProfileConfigKey:
			raw, _ := json.Marshal(testProfile())
			value = string(raw)
		default:
			return nil, nil, errors.New("unexpected configuration key")
		}
		raw, _ := json.Marshal(value)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	case portalapi.KernelCatalogValidationCapability:
		var request struct {
			TenantID string               `json:"tenantId"`
			Spec     portalapi.PortalSpec `json:"spec"`
		}
		if err := json.Unmarshal(payload, &request); err != nil || request.TenantID != "tenant-a" || request.Spec.ID != "admin" {
			return nil, nil, errors.New("unexpected catalog request")
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"valid":true}`), nil
	default:
		return nil, nil, errors.New("unexpected host capability")
	}
}

func TestContributionGetsStateAndCatalogOnlyFromAuthenticatedHost(t *testing.T) {
	service, err := New("", nil)
	if err != nil {
		t.Fatal(err)
	}
	host := &configuredHost{stateFile: filepath.Join(t.TempDir(), "portals.json")}
	callCtx := &contractv1.CallContext{
		TenantId:  "tenant-a",
		Principal: &contractv1.Principal{UserId: "author", SystemRoles: []string{"portal.compose"}},
	}
	payload, _ := json.Marshal(spec("/"))
	handler := Contribution(service).Handlers["createDraft"]
	result, raw, err := handler(context.Background(), host, callCtx, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("通过可信宿主创建草稿失败: result=%+v err=%v", result, err)
	}
	if len(host.calls) != 3 || host.calls[0] != "kernel.config.get" || host.calls[1] != "kernel.config.get" || host.calls[2] != portalapi.KernelCatalogValidationCapability {
		t.Fatalf("宿主调用路径错误: %v", host.calls)
	}
	var revision portalapi.Revision
	if err := json.Unmarshal(raw, &revision); err != nil || revision.ID != 1 {
		t.Fatalf("创建草稿响应错误: %s %v", raw, err)
	}
}
