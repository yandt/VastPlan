package portaltrust

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type ArtifactReferencePublisher interface {
	Publish(context.Context, *contractv1.CallContext, pluginv1.ArtifactReferenceSnapshot) error
}

// DevelopmentArtifactReferencePublisher performs contract validation without
// persisting references. It is available only when development plugins are
// explicitly enabled and must never be selected by production configuration.
type DevelopmentArtifactReferencePublisher struct{}

func (DevelopmentArtifactReferencePublisher) Publish(context.Context, *contractv1.CallContext, pluginv1.ArtifactReferenceSnapshot) error {
	return nil
}

type AddressingArtifactReferencePublisher struct{ router *addressing.Router }

func NewAddressingArtifactReferencePublisher(router *addressing.Router) (*AddressingArtifactReferencePublisher, error) {
	if router == nil {
		return nil, errors.New("Portal 制品引用 addressing router 不能为空")
	}
	return &AddressingArtifactReferencePublisher{router: router}, nil
}

func (p *AddressingArtifactReferencePublisher) Publish(ctx context.Context, callCtx *contractv1.CallContext, value pluginv1.ArtifactReferenceSnapshot) error {
	trusted, err := callcontext.ValidateIngress(callCtx, callcontext.Provenance{Source: "portal.trust.kernel-service", AuthenticatedBy: "protocolbus.host"})
	if err != nil {
		return fmt.Errorf("验证 Portal Composer 引用发布身份: %w", err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	operation, logicalService, routingDomain := "putReferences", platformadminapi.ArtifactsCapability, "platform"
	target := &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: platformadminapi.ArtifactsCapability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}
	result, _, err := p.router.Invoke(callcontext.WithTrusted(ctx, trusted), target, trusted.Wire(), raw)
	if err != nil {
		return fmt.Errorf("路由 Portal 制品引用快照: %w", err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return errors.New("远端制品仓库拒绝 Portal 引用快照")
	}
	return nil
}

func ArtifactReferencePublicationService(publisher ArtifactReferencePublisher) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if publisher == nil {
			return nil, nil, errors.New("Portal 制品引用集群发布器未配置")
		}
		if callCtx == nil || callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != portalapi.ComposerPluginID || callCtx.GetTenantId() == "" {
			return nil, nil, errors.New("Portal 制品引用只接受已认证 Composer 插件")
		}
		var request pluginv1.ArtifactReferenceSnapshot
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return nil, nil, err
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, nil, errors.New("Portal 制品引用请求只能包含一个 JSON 对象")
		}
		if request.OwnerKind != artifactreference.OwnerPortalActivation && request.OwnerKind != artifactreference.OwnerArtifactLock && request.OwnerKind != artifactreference.OwnerRollbackHistory {
			return nil, nil, errors.New("Portal Composer 无权声明该引用 owner kind")
		}
		if (request.OwnerKind == artifactreference.OwnerArtifactLock && !strings.HasPrefix(request.OwnerID, "portal/test-release-")) || (request.OwnerKind != artifactreference.OwnerArtifactLock && !strings.HasPrefix(request.OwnerID, "portal/")) {
			return nil, nil, errors.New("Portal 制品引用 owner ID 不属于 Composer 命名空间")
		}
		if err := artifactreference.Validate(request); err != nil {
			return nil, nil, err
		}
		if err := publisher.Publish(ctx, callCtx, request); err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"published":true}`), nil
	}
}
