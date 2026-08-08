package nodeagent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const stateMigrationProtocolV1 = "lifecycle.v1"

// planStateMigrations 只根据已经验签并安装的旧/新清单生成迁移计划。插件首次引入
// 状态无需迁移；已有状态被隐式改成 stateless，或新清单未声明旧格式时 fail-closed。
func planStateMigrations(unitID, fingerprint string, current, candidate []InstalledPlugin) ([]StateMigrationPlan, error) {
	oldByID := make(map[string]InstalledPlugin, len(current))
	for _, plugin := range current {
		oldByID[plugin.ID] = plugin
	}
	plans := make([]StateMigrationPlan, 0)
	for _, next := range candidate {
		previous, existed := oldByID[next.ID]
		if !existed || previous.State == nil {
			continue
		}
		if next.State == nil {
			return nil, fmt.Errorf("插件 %s 已有状态 %s@%d，新版本未声明 state.backend",
				next.ID, previous.State.Format, previous.State.FormatVersion)
		}
		from := previous.State.PluginStateIdentity
		to := next.State.PluginStateIdentity
		if from == to {
			continue
		}
		if next.State.MigrationProtocol != stateMigrationProtocolV1 || !acceptsMigrationFrom(next.State, from) {
			return nil, fmt.Errorf("插件 %s 状态 %s@%d -> %s@%d 未声明 lifecycle.v1 迁移来源",
				next.ID, from.Format, from.FormatVersion, to.Format, to.FormatVersion)
		}
		plans = append(plans, StateMigrationPlan{
			PluginID: next.ID, TransactionID: migrationTransactionID(unitID, fingerprint, next.ID, from, to),
			From: from, To: to,
		})
	}
	return plans, nil
}

func acceptsMigrationFrom(contract *PluginStateContract, from PluginStateIdentity) bool {
	for _, accepted := range contract.MigrationFrom {
		if accepted == from {
			return true
		}
	}
	return false
}

func migrationTransactionID(unitID, fingerprint, pluginID string, from, to PluginStateIdentity) string {
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%s\x00%d",
		unitID, fingerprint, pluginID, from.Format, from.FormatVersion, to.Format, to.FormatVersion)
	digest := sha256.Sum256([]byte(raw))
	return "migration-" + hex.EncodeToString(digest[:16])
}
