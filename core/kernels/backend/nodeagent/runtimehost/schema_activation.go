package runtimehost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

// SchemaActivationError keeps database migration failures distinct from
// plugin-private lifecycle migrations and ordinary process launch failures.
type SchemaActivationError struct {
	ModelID string
	Phase   string
	Err     error
}

func (e *SchemaActivationError) Error() string {
	return fmt.Sprintf("DataModel %s schema %s 失败: %v", e.ModelID, e.Phase, e.Err)
}
func (e *SchemaActivationError) Unwrap() error { return e.Err }

func (transaction *applyTransaction) applyTrustedSchemaActivation(ctx context.Context) error {
	if transaction.modelInventory == nil || len(transaction.modelInventory.Models) == 0 {
		return nil
	}
	authorizations := map[string]recordstorev1.SchemaMigrationAuthorization{}
	activation := transaction.modelInventory.SchemaActivation
	// Inventory synchronization and schema activation are separate protocols.
	// A bootstrap Runtime must be able to accept the signed model directory
	// before the Platform Control Store exists; actual DDL is prepared by the
	// Bootstrap Controller when that store is configured. Plugin upgrades carry
	// an explicit SchemaActivation and continue through the fail-closed path below.
	if activation == nil {
		return nil
	}
	if activation.CandidateID == "" || activation.PlanDigest == "" ||
		(activation.Mode != recordstorev1.SchemaActivationApproved && activation.Mode != recordstorev1.SchemaActivationAutomatic) {
		return errors.New("可信 Schema Activation 身份无效")
	}
	if activation.Mode == recordstorev1.SchemaActivationApproved && activation.ApprovedBy == "" {
		return errors.New("生产 Schema Activation 缺少审批主体")
	}
	for _, item := range activation.Models {
		if _, duplicate := authorizations[item.Model.ID]; duplicate {
			return fmt.Errorf("Schema Activation 重复授权模型 %s", item.Model.ID)
		}
		authorizations[item.Model.ID] = item
	}
	for _, signed := range transaction.modelInventory.Models {
		model, err := decodeInventoryModel(signed)
		if err != nil {
			return &SchemaActivationError{ModelID: signed.Model.ID, Phase: "decode", Err: err}
		}
		authorization, authorized := authorizations[signed.Model.ID]
		storage := recordstorev1.StorageTarget{}
		if authorized {
			if authorization.Model != signed.Model {
				return &SchemaActivationError{ModelID: signed.Model.ID, Phase: "authorize", Err: errors.New("授权模型身份与候选制品不一致")}
			}
			storage = authorization.Storage
		} else if model.Storage.Kind == "connection-ref" {
			return &SchemaActivationError{ModelID: signed.Model.ID, Phase: "authorize", Err: errors.New("connection-ref 模型缺少精确存储绑定")}
		}
		plan, err := transaction.schemaCall(ctx, recordstorev1.OperationSchemaPlan, recordstorev1.SchemaRequest{Storage: storage, Model: signed.Model, MigrationID: authorization.MigrationID}, nil)
		if err != nil {
			return &SchemaActivationError{ModelID: signed.Model.ID, Phase: "plan", Err: err}
		}
		if plan.Kind == "none" {
			continue
		}
		if !authorized {
			return &SchemaActivationError{ModelID: signed.Model.ID, Phase: "authorize", Err: errors.New("数据库变更未绑定已审批发布计划")}
		}
		evidence, err := schemaEvidence(authorization)
		if err != nil {
			return &SchemaActivationError{ModelID: signed.Model.ID, Phase: "authorize", Err: err}
		}
		if plan.Kind != authorization.Kind {
			return &SchemaActivationError{ModelID: signed.Model.ID, Phase: "plan", Err: fmt.Errorf("运行时计划 %s 与已审批计划 %s 不一致", plan.Kind, authorization.Kind)}
		}
		if _, err := transaction.schemaCall(ctx, recordstorev1.OperationSchemaApply, recordstorev1.SchemaRequest{Storage: storage, Model: signed.Model, MigrationID: authorization.MigrationID}, evidence); err != nil {
			return &SchemaActivationError{ModelID: signed.Model.ID, Phase: "apply", Err: err}
		}
		status, err := transaction.schemaStatus(ctx, recordstorev1.SchemaRequest{Storage: storage, Model: signed.Model})
		if err != nil || !status.Ready || status.SchemaVersion != signed.Model.SchemaVersion || status.SHA256 != signed.Model.SHA256 {
			if err == nil {
				err = errors.New("迁移后 Schema 状态与候选模型不一致")
			}
			return &SchemaActivationError{ModelID: signed.Model.ID, Phase: "verify", Err: err}
		}
	}
	return nil
}

func decodeInventoryModel(signed recordstorev1.SignedModel) (datamodelv1.Model, error) {
	document, err := base64.StdEncoding.DecodeString(signed.DocumentBase64)
	if err != nil {
		return datamodelv1.Model{}, err
	}
	return datamodelv1.Parse(document)
}

func schemaEvidence(authorization recordstorev1.SchemaMigrationAuthorization) ([]string, error) {
	resourceID := platformcontrolv1.DatabaseConnectionResourceID
	if authorization.Storage.Connection != nil {
		resourceID = authorization.Storage.Connection.ResourceID
	}
	evidence := []string{recordstorev1.SchemaControllerEvidence(resourceID)}
	switch authorization.Kind {
	case "create", "additive":
		if !authorization.AllowSafe {
			return nil, errors.New("安全迁移未获授权")
		}
	case "signed":
		if !authorization.AllowSigned || authorization.MigrationID == "" || authorization.BackupRef == "" {
			return nil, errors.New("签名迁移缺少审批或备份证据")
		}
		base := resourceID + "/" + authorization.Model.ID + "/" + authorization.MigrationID
		evidence = append(evidence, "database.schema-backup/"+base, "database.schema-approval/"+base)
	default:
		return nil, errors.New("Schema Activation 迁移类型无效")
	}
	return evidence, nil
}

func (transaction *applyTransaction) schemaCall(ctx context.Context, operation string, request recordstorev1.SchemaRequest, evidence []string) (recordstorev1.SchemaPlanResult, error) {
	var result recordstorev1.SchemaPlanResult
	payload, _ := json.Marshal(request)
	response, err := transaction.candidate.InvokeTrustedSystem(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: recordstorev1.Capability, Operation: &operation}, evidence, payload)
	if err != nil {
		return result, err
	}
	if response == nil || response.Result == nil || response.Result.Status != contractv1.CallResult_STATUS_OK {
		message := "Record Store 返回空结果"
		if response != nil && response.Result != nil && response.Result.Error != nil {
			message = response.Result.Error.GetMessage()
		}
		return result, errors.New(message)
	}
	if err := json.Unmarshal(response.Payload, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (transaction *applyTransaction) schemaStatus(ctx context.Context, request recordstorev1.SchemaRequest) (recordstorev1.SchemaStatusResult, error) {
	var result recordstorev1.SchemaStatusResult
	payload, _ := json.Marshal(request)
	operation := recordstorev1.OperationSchemaStatus
	response, err := transaction.candidate.InvokeTrustedSystem(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: recordstorev1.Capability, Operation: &operation}, nil, payload)
	if err != nil {
		return result, err
	}
	if response == nil || response.Result == nil || response.Result.Status != contractv1.CallResult_STATUS_OK {
		message := "Record Store 返回空结果"
		if response != nil && response.Result != nil && response.Result.Error != nil {
			message = response.Result.Error.GetMessage()
		}
		return result, errors.New(message)
	}
	if err := json.Unmarshal(response.Payload, &result); err != nil {
		return result, err
	}
	return result, nil
}
