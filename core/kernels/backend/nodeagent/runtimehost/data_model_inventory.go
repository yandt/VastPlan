package runtimehost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

func extractTrustedDataModelInventory(plugins []InstalledPlugin, values map[string]map[string]any) (*recordstorev1.SyncModelsRequest, error) {
	providers := map[string]struct{}{}
	for _, plugin := range plugins {
		for _, contribution := range plugin.Contract.Contributions {
			if contribution.ID == recordstorev1.Capability && contribution.ContractVersion == recordstorev1.ContractVersion {
				providers[plugin.ID] = struct{}{}
			}
		}
	}
	var inventory *recordstorev1.SyncModelsRequest
	for pluginID, configuration := range values {
		rawValue, exists := configuration[recordstorev1.TrustedInventoryConfigKey]
		if !exists {
			continue
		}
		if _, provider := providers[pluginID]; !provider {
			return nil, fmt.Errorf("插件 %s 不提供 %s，不能接收宿主 DataModel Inventory", pluginID, recordstorev1.Capability)
		}
		if inventory != nil {
			return nil, errors.New("一个 service unit 只能接收一份可信 DataModel Inventory")
		}
		raw, err := json.Marshal(rawValue)
		if err != nil {
			return nil, fmt.Errorf("编码可信 DataModel Inventory: %w", err)
		}
		parsed, err := recordstorev1.ParseRequest(recordstorev1.OperationSyncModels, raw)
		if err != nil {
			return nil, fmt.Errorf("校验可信 DataModel Inventory: %w", err)
		}
		request := parsed.(*recordstorev1.SyncModelsRequest)
		inventory = request
		delete(configuration, recordstorev1.TrustedInventoryConfigKey)
	}
	return inventory, nil
}

func (transaction *applyTransaction) syncTrustedDataModelInventory(ctx context.Context) error {
	if transaction.modelInventory == nil {
		return nil
	}
	payload, err := json.Marshal(transaction.modelInventory)
	if err != nil {
		return fmt.Errorf("编码可信 DataModel Inventory: %w", err)
	}
	operation := recordstorev1.OperationSyncModels
	response, err := transaction.candidate.InvokeTrustedSystem(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: recordstorev1.Capability, Operation: &operation,
	}, []string{"plugin.inventory/" + transaction.modelInventory.InventoryDigest}, payload)
	if err != nil {
		return fmt.Errorf("同步可信 DataModel Inventory: %w", err)
	}
	if response == nil || response.Result == nil || response.Result.Status != contractv1.CallResult_STATUS_OK {
		message := "Record Store 返回空结果"
		if response != nil && response.Result != nil && response.Result.Error != nil {
			message = response.Result.Error.GetMessage()
		}
		return fmt.Errorf("同步可信 DataModel Inventory 被拒绝: %s", message)
	}
	return nil
}
