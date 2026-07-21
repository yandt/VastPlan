package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactstorage"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/repositoryruntime"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func migrationHandler(manager *repositoryruntime.Manager, operation string) sdk.Handler {
	return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		view, err := handleMigration(ctx, host, call, manager, operation, payload)
		if err != nil {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.artifacts.migration_failed", Message: err.Error(), Retryable: operation == "syncMigration" || operation == "cutoverMigration"}}, nil, nil
		}
		raw, err := json.Marshal(view)
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

func handleMigration(ctx context.Context, host sdk.Host, call *contractv1.CallContext, manager *repositoryruntime.Manager, operation string, payload []byte) (repositoryruntime.MigrationView, error) {
	if manager == nil {
		return repositoryruntime.MigrationView{}, errors.New("制品迁移控制器不可用")
	}
	if operation == "migrationStatus" {
		if err := requireEmptyObject(payload); err != nil {
			return repositoryruntime.MigrationView{}, err
		}
		return manager.Migration(), nil
	}
	var request struct {
		MigrationID        string `json:"migrationId"`
		TargetProvider     string `json:"targetProvider,omitempty"`
		TargetVolumeID     string `json:"targetVolumeId,omitempty"`
		ObservationSeconds int64  `json:"observationSeconds,omitempty"`
	}
	if err := decodeMigrationPayload(payload, &request); err != nil {
		return repositoryruntime.MigrationView{}, err
	}
	if err := artifactstorage.ValidateMigrationID(request.MigrationID); err != nil {
		return repositoryruntime.MigrationView{}, err
	}

	switch operation {
	case "prepareMigration":
		if host == nil || request.TargetProvider == "" || request.TargetVolumeID == "" {
			return repositoryruntime.MigrationView{}, errors.New("prepareMigration 需要可用宿主、targetProvider 和 targetVolumeId")
		}
		if err := artifactstorage.ValidateProviderID(request.TargetProvider); err != nil {
			return repositoryruntime.MigrationView{}, err
		}
		if err := artifactstorage.ValidateVolumeID(request.TargetVolumeID); err != nil {
			return repositoryruntime.MigrationView{}, err
		}
		active := manager.ActiveVolume()
		if request.TargetProvider != active.ProviderID {
			return repositoryruntime.MigrationView{}, errors.New("v1 只支持同一 File Provider 内的 volume A/B 迁移")
		}
		providerRequest := artifactstorage.VolumeMigrationRequest{MigrationID: request.MigrationID, SourceVolumeID: active.VolumeID, TargetVolumeID: request.TargetVolumeID, Phase: artifactstorage.MigrationPrepare}
		var result artifactstorage.VolumeMigrationResult
		if err := callStorageProvider(ctx, host, call, request.TargetProvider, "migrate", providerRequest, &result); err != nil {
			return repositoryruntime.MigrationView{}, err
		}
		return manager.Prepare(result)
	case "syncMigration":
		provider, providerRequest, err := manager.ProviderMigrationRequest(request.MigrationID, artifactstorage.MigrationSync)
		if err != nil {
			return repositoryruntime.MigrationView{}, err
		}
		var result artifactstorage.VolumeMigrationResult
		if err := callStorageProvider(ctx, host, call, provider, "migrate", providerRequest, &result); err != nil {
			return repositoryruntime.MigrationView{}, err
		}
		return manager.MarkSynced(result)
	case "cutoverMigration":
		if request.ObservationSeconds < 60 || request.ObservationSeconds > 7*24*60*60 {
			return repositoryruntime.MigrationView{}, errors.New("observationSeconds 必须在 60 秒到 7 天之间")
		}
		provider, providerRequest, err := manager.ProviderMigrationRequest(request.MigrationID, artifactstorage.MigrationSync)
		if err != nil {
			return repositoryruntime.MigrationView{}, err
		}
		return manager.Cutover(ctx, request.MigrationID, time.Duration(request.ObservationSeconds)*time.Second, func(syncContext context.Context) (artifactstorage.VolumeMigrationResult, error) {
			var result artifactstorage.VolumeMigrationResult
			err := callStorageProvider(syncContext, host, call, provider, "migrate", providerRequest, &result)
			return result, err
		})
	case "rollbackMigration":
		return manager.Rollback(request.MigrationID)
	case "finalizeMigration":
		return manager.Finalize(request.MigrationID, time.Now().UTC())
	case "releaseMigration":
		provider, _, err := manager.ProviderMigrationRequest(request.MigrationID, artifactstorage.MigrationVerify)
		if err != nil {
			return repositoryruntime.MigrationView{}, err
		}
		releaseRequest, err := manager.SourceReleaseRequest(request.MigrationID)
		if err != nil {
			return repositoryruntime.MigrationView{}, err
		}
		var result artifactstorage.VolumeReleaseResult
		if err := callStorageProvider(ctx, host, call, provider, "release", releaseRequest, &result); err != nil {
			return repositoryruntime.MigrationView{}, err
		}
		return manager.MarkReleased(request.MigrationID, result)
	default:
		return repositoryruntime.MigrationView{}, fmt.Errorf("不支持的仓库迁移操作 %q", operation)
	}
}

func callStorageProvider(ctx context.Context, host sdk.Host, call *contractv1.CallContext, provider, operation string, input, output any) error {
	if host == nil {
		return errors.New("制品迁移缺少可验证宿主")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	logicalService, routingDomain := platformadminapi.ArtifactsCapability, "platform"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: provider, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}, call, payload)
	if err != nil {
		return err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		if result != nil && result.Error != nil && result.Error.Message != "" {
			return errors.New(result.Error.Message)
		}
		return errors.New("制品存储 Provider 调用失败")
	}
	return decodeMigrationPayload(raw, output)
}

func requireEmptyObject(raw []byte) error {
	var value map[string]json.RawMessage
	if err := decodeMigrationPayload(raw, &value); err != nil {
		return err
	}
	if len(value) != 0 {
		return errors.New("该操作不接受参数")
	}
	return nil
}

func decodeMigrationPayload(raw []byte, output any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("迁移请求只能包含一个 JSON 值")
	}
	return nil
}
