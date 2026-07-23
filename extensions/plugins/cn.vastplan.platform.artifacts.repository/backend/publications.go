package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/repositoryruntime"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func publicationHandlers(manager *repositoryruntime.Manager) map[string]sdk.Handler {
	return map[string]sdk.Handler{
		"listPublications": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			if err := requireEmptyObject(raw); err != nil {
				return nil, nil, err
			}
			page, err := manager.Publications()
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(page)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"submitPublication": func(_ context.Context, _ sdk.Host, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			principal, err := publicationActor(call)
			if err != nil {
				return nil, nil, err
			}
			var request catalog.PublicationRequest
			if err := decodeParams(raw, &request); err != nil {
				return nil, nil, err
			}
			record, revision, err := manager.SubmitPublication(request, principal, time.Now().UTC())
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(map[string]any{"revision": revision, "entry": record})
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"approvePublication": func(_ context.Context, _ sdk.Host, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			principal, err := publicationActor(call)
			if err != nil {
				return nil, nil, err
			}
			var request catalog.PublicationApprovalRequest
			if err := decodeParams(raw, &request); err != nil {
				return nil, nil, err
			}
			record, revision, err := manager.ApprovePublication(request, principal, time.Now().UTC())
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(map[string]any{"revision": revision, "entry": record})
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"rejectPublication": publicationTransitionHandler(manager.RejectPublication),
		"cancelPublication": publicationTransitionHandler(manager.CancelPublication),
		"getSupplyChainEvidence": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			var ref pluginv1.ArtifactRef
			if err := decodeParams(raw, &ref); err != nil {
				return nil, nil, err
			}
			evidence, err := manager.SupplyChainEvidence(ref)
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(evidence)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
	}
}

func publicationTransitionHandler(transition func(catalog.PublicationTransitionRequest, string, time.Time) (catalog.Publication, uint64, error)) sdk.Handler {
	return func(_ context.Context, _ sdk.Host, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
		principal, err := publicationActor(call)
		if err != nil {
			return nil, nil, err
		}
		var request catalog.PublicationTransitionRequest
		if err := decodeParams(raw, &request); err != nil {
			return nil, nil, err
		}
		record, revision, err := transition(request, principal, time.Now().UTC())
		if err != nil {
			return nil, nil, err
		}
		payload, err := json.Marshal(map[string]any{"revision": revision, "entry": record})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
	}
}

func publicationActor(call *contractv1.CallContext) (string, error) {
	if call == nil {
		return "", errors.New("发布审批必须由可信用户发起")
	}
	actor := strings.TrimSpace(call.GetPrincipal().GetUserId())
	if actor == "" && call.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_USER {
		actor = strings.TrimSpace(call.GetCaller().GetId())
	}
	if actor == "" {
		return "", errors.New("发布审批必须由可信用户发起")
	}
	return actor, nil
}
