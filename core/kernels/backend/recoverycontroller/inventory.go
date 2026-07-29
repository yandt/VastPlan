package recoverycontroller

import (
	"errors"
	"fmt"

	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
)

func ValidateInventory(capsule recoveryv1.Capsule, inventory bootstrapinventory.Inventory) error {
	if capsule.Inventory.RepositoryID != inventory.RepositoryID || capsule.Inventory.Generation != inventory.Generation {
		return errors.New("Recovery Capsule 与 Bootstrap Inventory 身份不一致")
	}
	if len(capsule.Artifacts) != len(inventory.LastKnownGood) {
		return errors.New("Recovery Capsule 与 Bootstrap LKG 数量不一致")
	}
	for index := range capsule.Artifacts {
		artifact, item := capsule.Artifacts[index], inventory.LastKnownGood[index]
		if artifact.Ref != item.Ref || artifact.SHA256 != item.SHA256 {
			return fmt.Errorf("Recovery Capsule 与 Bootstrap LKG 不一致: %s", artifact.Ref.PluginID)
		}
	}
	return nil
}
