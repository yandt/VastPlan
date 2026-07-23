package pluginconfiguration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const MaxValuesBytes = 64 << 10

type CandidateStatus string

const (
	CandidateDraft       CandidateStatus = "Draft"
	CandidatePreparing   CandidateStatus = "Preparing"
	CandidatePublishing  CandidateStatus = "Publishing"
	CandidateActivating  CandidateStatus = "Activating"
	CandidateReady       CandidateStatus = "Ready"
	CandidateFailed      CandidateStatus = "Failed"
	CandidateRollingBack CandidateStatus = "RollingBack"
	CandidateRolledBack  CandidateStatus = "RolledBack"
)

type ManagedCredentialStatus struct {
	FieldID string `json:"fieldId"`
	Staged  bool   `json:"staged"`
	State   string `json:"state"`
}

// Candidate never contains secret material, authority tokens, stage IDs or
// managed credential handles. Only browser-safe field status is returned.
type Candidate struct {
	ID                   string                    `json:"id"`
	ConfigurationID      string                    `json:"configurationId"`
	ResourceCollectionID string                    `json:"resourceCollectionId,omitempty"`
	ResourceID           string                    `json:"resourceId,omitempty"`
	ResourceAction       string                    `json:"resourceAction,omitempty"`
	Revision             uint64                    `json:"revision"`
	Status               CandidateStatus           `json:"status"`
	ApplyPath            ApplyPath                 `json:"applyPath"`
	CatalogDigest        string                    `json:"catalogDigest"`
	SchemaDigest         string                    `json:"schemaDigest"`
	ArtifactSHA256       string                    `json:"artifactSha256"`
	Values               json.RawMessage           `json:"values"`
	CreatedBy            string                    `json:"createdBy"`
	CreatedAt            string                    `json:"createdAt"`
	UpdatedAt            string                    `json:"updatedAt"`
	ErrorCode            string                    `json:"errorCode,omitempty"`
	ErrorMessage         string                    `json:"errorMessage,omitempty"`
	ExternalRevision     uint64                    `json:"externalRevision,omitempty"`
	ExternalDigest       string                    `json:"externalDigest,omitempty"`
	ExternalStatus       string                    `json:"externalStatus,omitempty"`
	RollbackRevision     uint64                    `json:"rollbackRevision,omitempty"`
	ManagedCredentials   []ManagedCredentialStatus `json:"managedCredentials,omitempty"`
}

func ValidApplyPath(path ApplyPath) bool {
	switch path {
	case ApplyApplicationDeployment, ApplyPlatformProfile, ApplyHotService, ApplyHotScoped, ApplyResourceProfile:
		return true
	default:
		return false
	}
}

type CreateDraftRequest struct {
	ConfigurationID string            `json:"configurationId"`
	CatalogDigest   string            `json:"catalogDigest"`
	Values          json.RawMessage   `json:"values"`
	Secrets         map[string]string `json:"secrets,omitempty"`
}

// ValidateValues evaluates the exact schema frozen in the trusted catalog.
// Remote refs were already rejected when the signed manifest was parsed.
func ValidateValues(definition Definition, raw json.RawMessage) error {
	return validateValues(definition.Schema, definition.SchemaDigest, raw)
}

func ValidateResourceValues(collection ResourceCollection, raw json.RawMessage) error {
	return validateValues(collection.Schema, collection.SchemaDigest, raw)
}

func validateValues(schemaRaw json.RawMessage, schemaDigest string, raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > MaxValuesBytes || !json.Valid(raw) {
		return errors.New("配置 values 必须是大小受限的有效 JSON")
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return errors.New("配置 values 必须是 JSON 对象")
	}
	compiler := jsonschema.NewCompiler()
	url := "https://schemas.cdsoft.com.cn/vastplan/plugin-configuration/" + schemaDigest + ".json"
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaRaw))
	if err != nil {
		return fmt.Errorf("解析配置 Schema: %w", err)
	}
	if err := compiler.AddResource(url, document); err != nil {
		return fmt.Errorf("加载配置 Schema: %w", err)
	}
	schema, err := compiler.Compile(url)
	if err != nil {
		return fmt.Errorf("编译配置 Schema: %w", err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析配置 values: %w", err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("配置 values 不符合签名 Schema: %w", err)
	}
	return nil
}

func ValidCandidateStatus(status CandidateStatus) bool {
	switch status {
	case CandidateDraft, CandidatePreparing, CandidatePublishing, CandidateActivating, CandidateReady, CandidateFailed, CandidateRollingBack, CandidateRolledBack:
		return true
	default:
		return false
	}
}
