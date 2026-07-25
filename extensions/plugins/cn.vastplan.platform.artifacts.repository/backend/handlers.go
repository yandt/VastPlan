package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/repositoryruntime"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func repositoryHandlers(config serverConfig, manager *repositoryruntime.Manager, transport *runningRepositoryTransport, registrar *dataPlaneLeaseRegistrar) map[string]sdk.Handler {
	handlers := publicationHandlers(manager)
	for operation, handler := range coreRepositoryHandlers(config, manager, transport, registrar) {
		handlers[operation] = handler
	}
	for operation, handler := range lifecycleRepositoryHandlers(manager) {
		handlers[operation] = handler
	}
	return handlers
}

func coreRepositoryHandlers(config serverConfig, manager *repositoryruntime.Manager, transport *runningRepositoryTransport, leaseRegistrar *dataPlaneLeaseRegistrar) map[string]sdk.Handler {
	tickets, assessmentLeases := transport.tickets, transport.assessmentLeases
	return map[string]sdk.Handler{
		"status": func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			leaseRegistrar.ensure(ctx, host, callCtx)
			status, marshalErr := json.Marshal(map[string]any{"protocol": config.profile.Protocol, "endpoint": config.profile.Endpoint, "ready": transport.ready.Load(), "storageProvider": config.storageProvider, "storageVolumeId": manager.ActiveVolume().VolumeID, "catalog": manager.Stats(), "securityAssessment": manager.SecurityAssessmentStats(time.Now().UTC()), "migration": manager.Migration(), "dataPlaneLease": leaseRegistrar.status()})
			if marshalErr != nil {
				return nil, nil, marshalErr
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, status, nil
		},
		"assessmentInventory": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			payload, err := json.Marshal(manager.AssessmentInventory(time.Now().UTC()))
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"prepareAssessmentReport": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			var request struct {
				SHA256 string `json:"sha256"`
			}
			if err := decodeParams(raw, &request); err != nil {
				return nil, nil, err
			}
			resource, err := manager.PrepareAssessmentReport(request.SHA256)
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(map[string]string{"sha256": request.SHA256, "resource": resource})
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"installDataPlaneTicket": func(_ context.Context, _ sdk.Host, callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			if tickets == nil {
				return nil, nil, errors.New("制品仓库未启用 API Exposure 数据面")
			}
			if err := tickets.install(callCtx, raw); err != nil {
				return nil, nil, err
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"installed":true}`), nil
		},
		"installAssessmentReportTicket": func(_ context.Context, _ sdk.Host, callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			if tickets == nil {
				return nil, nil, errors.New("制品仓库未启用 API Exposure 数据面")
			}
			if err := tickets.installAssessmentReport(callCtx, raw); err != nil {
				return nil, nil, err
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"installed":true}`), nil
		},
		"prepareAssessment": func(_ context.Context, _ sdk.Host, callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			if assessmentLeases == nil {
				return nil, nil, errors.New("制品仓库未启用安全评估数据面")
			}
			lease, err := assessmentLeases.issue(callCtx, raw)
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(lease)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"appendAssessmentStatus": func(_ context.Context, _ sdk.Host, callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			payload, err := appendAssessmentStatus(manager, callCtx, raw, time.Now().UTC())
			if err != nil {
				return nil, nil, err
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, nil
		},
		"capacity": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			payload, err := json.Marshal(manager.Capacity())
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"listCatalog": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			var request struct {
				Receipt      *artifactrepositoryv1.Receipt `json:"receipt,omitempty"`
				PluginID     string                        `json:"pluginId"`
				PluginPrefix string                        `json:"pluginPrefix"`
				Namespace    string                        `json:"namespace"`
				Publisher    string                        `json:"publisher"`
				Version      string                        `json:"version"`
				Channel      string                        `json:"channel"`
				Target       string                        `json:"target"`
				Lifecycle    string                        `json:"lifecycle"`
				Page         int                           `json:"page"`
				PageSize     int                           `json:"pageSize"`
			}
			if err := decodeParams(raw, &request); err != nil {
				return nil, nil, err
			}
			if request.Receipt != nil {
				if request.Target != "backend" && request.Target != "frontend" {
					return nil, nil, errors.New("回执验证 target 无效")
				}
				entry, err := transport.validateReceipt(config.profile, manager, *request.Receipt, request.Target)
				if err != nil {
					return nil, nil, err
				}
				payload, err := json.Marshal(entry)
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
			}
			response := manager.Query(catalog.Query{
				PluginID: request.PluginID, PluginPrefix: request.PluginPrefix, Namespace: request.Namespace,
				Publisher: request.Publisher, Version: request.Version, Channel: request.Channel,
				Target: request.Target, Lifecycle: request.Lifecycle, Page: request.Page, PageSize: request.PageSize,
			})
			payload, err := json.Marshal(response)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"listPublishJournal": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			var request struct {
				AfterRevision uint64 `json:"afterRevision"`
				Limit         int    `json:"limit"`
			}
			if err := decodeParams(raw, &request); err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(manager.Journal(request.AfterRevision, request.Limit))
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"resolve": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			var request pluginv1.ArtifactResolveRequest
			if err := decodeParams(raw, &request); err != nil {
				return nil, nil, err
			}
			lock, err := manager.Resolve(request)
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(lock)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"describePlanning": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			request, err := pluginv1.ParseArtifactPlanningRequest(raw)
			if err != nil {
				return nil, nil, err
			}
			response, err := manager.DescribePlanning(request)
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(response)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
	}
}

func lifecycleRepositoryHandlers(manager *repositoryruntime.Manager) map[string]sdk.Handler {
	return map[string]sdk.Handler{
		"setLifecycle": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			var request catalog.LifecycleRequest
			if err := decodeParams(raw, &request); err != nil {
				return nil, nil, err
			}
			entry, revision, err := manager.SetLifecycle(request, time.Now().UTC())
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(map[string]any{"revision": revision, "entry": entry})
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"putReferences": func(_ context.Context, _ sdk.Host, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			if call == nil || call.GetTenantId() == "" || call.GetCaller().GetId() == "" || (call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN && call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM) {
				return nil, nil, errors.New("引用快照必须由可信插件或内核服务身份发布")
			}
			var request pluginv1.ArtifactReferenceSnapshot
			if err := decodeParams(raw, &request); err != nil {
				return nil, nil, err
			}
			if !referenceOwnerAllowed(call.GetCaller().GetId(), request.OwnerKind) || !referenceOwnerIDAllowed(call.GetCaller().GetId(), request.OwnerKind, request.OwnerID) {
				return nil, nil, errors.New("调用者无权声明该引用 owner kind")
			}
			snapshot, revision, err := manager.PutReferences(call.GetTenantId(), call.GetCaller().GetId(), request, time.Now().UTC())
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(map[string]any{"revision": revision, "snapshot": snapshot})
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"listReferences": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			revision, snapshots := manager.References()
			payload, err := json.Marshal(map[string]any{"revision": revision, "items": snapshots})
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"gcPlan": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			payload, err := json.Marshal(manager.PlanGarbageCollection(time.Now().UTC()))
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"gcStatus": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			payload, err := json.Marshal(manager.GarbageCollectionStatus())
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"gcQuarantine": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			var request struct {
				PlanID     string `json:"planId"`
				GraceHours int64  `json:"graceHours"`
			}
			if err := decodeParams(raw, &request); err != nil {
				return nil, nil, err
			}
			if request.PlanID == "" || request.GraceHours < 24 || request.GraceHours > 24*365 {
				return nil, nil, errors.New("GC planId 或 24..8760 小时宽限期无效")
			}
			status, err := manager.QuarantineGarbageCollection(request.PlanID, time.Duration(request.GraceHours)*time.Hour, time.Now().UTC())
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(status)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"gcSweep": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			status, err := manager.SweepGarbageCollection(time.Now().UTC())
			if err != nil {
				return nil, nil, err
			}
			payload, err := json.Marshal(status)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
		},
		"migrationStatus":   migrationHandler(manager, "migrationStatus"),
		"prepareMigration":  migrationHandler(manager, "prepareMigration"),
		"syncMigration":     migrationHandler(manager, "syncMigration"),
		"cutoverMigration":  migrationHandler(manager, "cutoverMigration"),
		"rollbackMigration": migrationHandler(manager, "rollbackMigration"),
		"finalizeMigration": migrationHandler(manager, "finalizeMigration"),
		"releaseMigration":  migrationHandler(manager, "releaseMigration"),
	}
}
