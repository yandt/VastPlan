package portalcomposer

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"cdsoft.com.cn/VastPlan/shared/go/portalapi"
)

type acceptingCatalog struct{}

func (acceptingCatalog) ValidatePortal(context.Context, string, portalapi.PortalSpec) error {
	return nil
}

func principal(id string, roles ...string) portalapi.Principal {
	return portalapi.Principal{ID: id, TenantID: "tenant-a", Roles: roles}
}
func spec(route string) portalapi.PortalSpec {
	ds := portalapi.PluginRef{ID: "com.vastplan.foundation.frontend.design-system.arco", Version: "1.0.0"}
	return portalapi.PortalSpec{ID: "admin", Route: route, DesignSystem: portalapi.DesignSystem{PluginRef: ds, UIContract: "^1.0.0"}, Plugins: []portalapi.PluginRef{ds}}
}

func TestGovernedPublishRequiresDifferentApproverAndPersistsAudit(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "portals.json"), acceptingCatalog{})
	if err != nil {
		t.Fatal(err)
	}
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
	} else if got, err := reopened.List(context.Background(), publisher); err != nil || len(got) != 1 || got[0].Status != portalapi.StatusPublished {
		t.Fatalf("持久化状态错误: %+v %v", got, err)
	}
}

func TestPublishRejectsCrossPortalRouteAndBreakGlassNeedsReason(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "portals.json"), acceptingCatalog{})
	if err != nil {
		t.Fatal(err)
	}
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
