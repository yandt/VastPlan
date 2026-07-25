//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/compositionresolver"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/deploymentpublisher"
	"cdsoft.com.cn/VastPlan/core/shared/go/compositioncore"
	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfiguration"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	plannerplugin "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.infrastructure.composition-planner/planner"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.infrastructure.deployment-manager/deploymentmanager"
)

const p5DeploymentStateNamespace = "deployment.control"

type p5PipelineHost struct {
	store     sharedstate.Store
	repo      *p5FixtureRepository
	planner   *plannerplugin.Service
	publisher *deploymentpublisher.Publisher
	applier   *p5DeploymentApplier
	catalogs  *p5CatalogPublisher
}

type p5DeploymentApplier struct {
	mu         sync.Mutex
	revision   uint64
	deployment deploymentv2.Deployment
}

func (a *p5DeploymentApplier) Apply(_ context.Context, _ string, raw []byte) (uint64, deploymentv2.Deployment, error) {
	parsed, err := deploymentv2.Parse(raw)
	if err != nil {
		return 0, deploymentv2.Deployment{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deployment.Revision >= parsed.Revision {
		return 0, deploymentv2.Deployment{}, fmt.Errorf("P5 Deployment revision 未单调递增: current=%d candidate=%d", a.deployment.Revision, parsed.Revision)
	}
	a.revision++
	a.deployment = parsed
	return a.revision, parsed, nil
}

type p5CatalogPublisher struct {
	mu      sync.Mutex
	tenants map[string]pluginconfiguration.Catalog
}

func (p *p5CatalogPublisher) Publish(_ context.Context, tenant string, catalog pluginconfiguration.Catalog) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tenants[tenant] = catalog
	return nil
}

func newP5PipelineHost(t *testing.T, repository *p5FixtureRepository) *p5PipelineHost {
	t.Helper()
	profile := p5PlatformProfile()
	profileRef := compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()}
	catalog, err := backendcompositionv1.ValidateBackendPlatformCatalog(backendcompositionv1.BackendPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "p5-platform-catalog"},
		Profiles: []backendcompositionv1.PlatformProfile{profile},
		Bindings: []backendcompositionv1.BackendPlatformBinding{{
			TenantID: "acme", DeploymentName: "p5-pipeline", PlatformProfile: profileRef,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sharedstate.OpenFileStore(filepath.Join(t.TempDir(), "deployment-manager-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	applier := &p5DeploymentApplier{}
	catalogs := &p5CatalogPublisher{tenants: map[string]pluginconfiguration.Catalog{}}
	publisher, err := deploymentpublisher.New(
		catalog,
		repository,
		applier,
		catalogs,
		compositioncore.Options{AllowDevelopmentPlugins: true},
		compositionresolver.Resolve,
	)
	if err != nil {
		t.Fatal(err)
	}
	return &p5PipelineHost{store: store, repo: repository, planner: newP5Planner(t), publisher: publisher, applier: applier, catalogs: catalogs}
}

func (h *p5PipelineHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	capability := target.GetCapability()
	if strings.HasPrefix(capability, sharedstatev1.KernelServicePrefix) {
		return h.callSharedState(ctx, strings.TrimPrefix(capability, sharedstatev1.KernelServicePrefix), call, payload)
	}
	switch capability {
	case backendcompositionv1.PlanningCapability:
		handler := plannerplugin.Contribution(h.planner).Handlers[backendcompositionv1.PlanningOperation]
		trustedCall := &contractv1.CallContext{
			TenantId: call.GetTenantId(),
			Caller:   &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: deploymentmanager.PluginID},
		}
		return handler(ctx, h, trustedCall, payload)
	case "platform.artifacts.repository":
		switch target.GetOperation() {
		case "describePlanning":
			var request pluginv1.ArtifactPlanningRequest
			if err := json.Unmarshal(payload, &request); err != nil {
				return nil, nil, err
			}
			response, err := h.repo.Describe(ctx, request)
			return p5JSONResult(response, err)
		case "resolve":
			var request pluginv1.ArtifactResolveRequest
			if err := json.Unmarshal(payload, &request); err != nil {
				return nil, nil, err
			}
			response, err := h.repo.Resolve(ctx, request)
			return p5JSONResult(response, err)
		case "putReferences":
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
		default:
			return nil, nil, fmt.Errorf("P5 仓库宿主不支持 operation %q", target.GetOperation())
		}
	case deploymentpublication.KernelTargetsService:
		items, err := h.publisher.Targets(ctx, call.GetTenantId())
		return p5JSONResult(map[string]any{"items": items}, err)
	case deploymentpublication.KernelPreviewService:
		var request deploymentpublication.PreviewRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, nil, err
		}
		result, err := h.publisher.Preview(ctx, call.GetTenantId(), request.Composition, request.DeploymentRevision)
		return p5JSONResult(result, err)
	case deploymentpublication.KernelPublishService:
		var request deploymentpublication.PublishRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, nil, err
		}
		result, err := h.publisher.Publish(ctx, call.GetTenantId(), request.Composition, request.DeploymentRevision, request.ExpectedDigest)
		return p5JSONResult(result, err)
	default:
		return nil, nil, fmt.Errorf("P5 测试宿主不支持 capability %q", capability)
	}
}

func (h *p5PipelineHost) callSharedState(ctx context.Context, operation string, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	request, err := sharedstatev1.ParseRequest(operation, payload)
	if err != nil {
		return p5DomainResult("state.invalid", false), nil, nil
	}
	scope := sharedstate.Scope{
		Kind: sharedstate.ScopeTenant, TenantID: call.GetTenantId(), PluginID: deploymentmanager.PluginID,
		RuntimeScope: "platform-deployment", Namespace: p5DeploymentStateNamespace,
	}
	var entry sharedstate.Entry
	switch typed := request.(type) {
	case *sharedstatev1.KeyRequest:
		entry, err = h.store.Get(ctx, scope, typed.Key)
	case *sharedstatev1.WriteRequest:
		var value []byte
		value, err = sharedstatev1.DecodeValue(typed.Value)
		if err == nil && operation == sharedstatev1.OperationCreate {
			entry, err = h.store.Create(ctx, scope, typed.Key, value)
		} else if err == nil {
			entry, err = h.store.Update(ctx, scope, typed.Key, value, typed.ExpectedRevision)
		}
	default:
		err = sharedstate.ErrInvalid
	}
	if err != nil {
		switch {
		case errors.Is(err, sharedstate.ErrNotFound):
			return p5DomainResult("state.not_found", false), nil, nil
		case errors.Is(err, sharedstate.ErrConflict):
			return p5DomainResult("state.conflict", true), nil, nil
		default:
			return p5DomainResult("state.unavailable", true), nil, nil
		}
	}
	return p5JSONResult(sharedstatev1.Entry{
		Protocol: sharedstatev1.Protocol, Key: entry.Key, Value: sharedstatev1.EncodeValue(entry.Value), Revision: entry.Revision, UpdatedAt: entry.UpdatedAt,
	}, nil)
}

func p5JSONResult(value any, err error) (*contractv1.CallResult, []byte, error) {
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func p5DomainResult(code string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: code, Retryable: retryable}}
}

func p5PluginSettingsCall() *contractv1.CallContext {
	return &contractv1.CallContext{
		TenantId: "acme",
		Caller:   &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: pluginconfiguration.PluginSettingsID},
	}
}

func p5UserCall(user string) *contractv1.CallContext {
	return &contractv1.CallContext{
		TenantId: "acme",
		Caller:   &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: user},
		Principal: &contractv1.Principal{
			TenantId: "acme", UserId: user,
		},
	}
}

func p5Invoke(t *testing.T, service *deploymentmanager.Service, host *p5PipelineHost, call *contractv1.CallContext, operation string, request any, output any) *contractv1.CallResult {
	t.Helper()
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	result, raw, err := service.Handler(context.Background(), host, call, payload, operation)
	if err != nil {
		t.Fatal(err)
	}
	if result.GetStatus() == contractv1.CallResult_STATUS_OK && output != nil {
		if err := json.Unmarshal(raw, output); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

var _ deploymentpublisher.Applier = (*p5DeploymentApplier)(nil)
var _ pluginconfiguration.Publisher = (*p5CatalogPublisher)(nil)
var _ compositioncore.ArtifactReader = (*p5FixtureRepository)(nil)
