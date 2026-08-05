package recordstorev1

import (
	"errors"
	"regexp"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

var schemaDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidateSchemaActivation(activation *SchemaActivation) error {
	if activation == nil {
		return nil
	}
	if activation.CandidateID == "" || !schemaDigestPattern.MatchString(activation.PlanDigest) {
		return errors.New("Schema Activation 身份无效")
	}
	if activation.Mode != SchemaActivationApproved && activation.Mode != SchemaActivationAutomatic {
		return errors.New("Schema Activation 模式无效")
	}
	if activation.Mode == SchemaActivationApproved && activation.ApprovedBy == "" {
		return errors.New("已审批 Schema Activation 缺少审批主体")
	}
	seen := map[string]struct{}{}
	for _, item := range activation.Models {
		if item.Model.ID == "" || item.Model.SchemaVersion == 0 || !schemaDigestPattern.MatchString(item.Model.SHA256) {
			return errors.New("Schema Activation 模型身份无效")
		}
		if _, duplicate := seen[item.Model.ID]; duplicate {
			return errors.New("Schema Activation 模型重复")
		}
		seen[item.Model.ID] = struct{}{}
		if item.Storage.Connection != nil && databasev1.ValidateConnectionRef(*item.Storage.Connection) != nil {
			return errors.New("Schema Activation 存储绑定无效")
		}
		switch item.Kind {
		case "create", "additive":
			if !item.AllowSafe || item.AllowSigned || item.MigrationID != "" || item.BackupRef != "" {
				return errors.New("安全 Schema Activation 授权字段无效")
			}
		case "signed":
			if activation.Mode != SchemaActivationApproved || !item.AllowSigned || item.AllowSafe || item.MigrationID == "" || item.BackupRef == "" {
				return errors.New("签名 Schema Activation 缺少审批或备份证据")
			}
		default:
			return errors.New("Schema Activation 迁移类型无效")
		}
	}
	return nil
}
