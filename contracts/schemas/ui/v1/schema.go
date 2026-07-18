// Package uiv1 defines the serializable UI and interaction contracts shared by
// Web, Mobile, Runner and Backend. It deliberately contains no renderer API.
package uiv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	UIContractVersion          = "1.0.0"
	InteractionContractVersion = "1.0.0"
	JSONSchemaDialect          = "http://json-schema.org/draft-07/schema#"
	UISchemaURL                = "https://schemas.cdsoft.com.cn/vastplan/ui/v1/vastplan.ui.schema.json"
	InteractionSchemaURL       = "https://schemas.cdsoft.com.cn/vastplan/ui/v1/vastplan.interaction.schema.json"
)

//go:embed vastplan.ui.schema.json
var uiSchemaJSON []byte

//go:embed vastplan.interaction.schema.json
var interactionSchemaJSON []byte

type UICapability string

const (
	CapabilityLayout     UICapability = "layout"
	CapabilityMenu       UICapability = "menu"
	CapabilityOverlay    UICapability = "overlay"
	CapabilityForm       UICapability = "form"
	CapabilityData       UICapability = "data"
	CapabilityFeedback   UICapability = "feedback"
	CapabilityTheme      UICapability = "theme"
	CapabilityApproval   UICapability = "approval"
	CapabilityNavigation UICapability = "navigation"
)

type JSONSchema map[string]any
type FormUISchema map[string]any

type FormSchema struct {
	ID       string       `json:"id"`
	Schema   JSONSchema   `json:"schema"`
	UISchema FormUISchema `json:"uiSchema,omitempty"`
}

type InteractionKind string

const (
	InteractionConfirm      InteractionKind = "confirm"
	InteractionForm         InteractionKind = "form"
	InteractionApproval     InteractionKind = "approval"
	InteractionNotification InteractionKind = "notification"
	InteractionProgress     InteractionKind = "progress"
)

type InteractionSurface string

const (
	SurfaceFrontend    InteractionSurface = "frontend"
	SurfaceMobile      InteractionSurface = "mobile"
	SurfaceRunnerLocal InteractionSurface = "runner.local"
)

type InteractionSource struct {
	WorkflowRunID string `json:"workflowRunId,omitempty"`
	Capability    string `json:"capability"`
	Operation     string `json:"operation,omitempty"`
}

type InteractionRequest struct {
	ID               string               `json:"id"`
	ContractVersion  string               `json:"contractVersion"`
	Kind             InteractionKind      `json:"kind"`
	Source           InteractionSource    `json:"source"`
	TenantID         string               `json:"tenantId"`
	EligibleSubjects []string             `json:"eligibleSubjects"`
	AllowedSurfaces  []InteractionSurface `json:"allowedSurfaces"`
	Fallback         string               `json:"fallback,omitempty"`
	ExpiresAt        time.Time            `json:"expiresAt"`
	Title            string               `json:"title,omitempty"`
	Message          string               `json:"message,omitempty"`
	Form             *FormSchema          `json:"form,omitempty"`
}

type InteractionDecision string

const (
	DecisionAnswered InteractionDecision = "answered"
	DecisionRejected InteractionDecision = "rejected"
)

// InteractionResponse is the renderer-to-Broker payload. Credential values are
// never accepted here; a secret field may only be represented by a CredentialRef.
type InteractionResponse struct {
	InteractionID string              `json:"interactionId"`
	Decision      InteractionDecision `json:"decision"`
	Values        map[string]any      `json:"values,omitempty"`
	CredentialRef map[string]string   `json:"credentialRefs,omitempty"`
}

var (
	compileOnce        sync.Once
	uiSchema           *jsonschema.Schema
	interactionSchema  *jsonschema.Schema
	compileSchemaError error
)

func schemas() error {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		for url, raw := range map[string][]byte{UISchemaURL: uiSchemaJSON, InteractionSchemaURL: interactionSchemaJSON} {
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				compileSchemaError = fmt.Errorf("解析 Schema %s: %w", url, err)
				return
			}
			if err := compiler.AddResource(url, doc); err != nil {
				compileSchemaError = fmt.Errorf("登记 Schema %s: %w", url, err)
				return
			}
		}
		uiSchema, compileSchemaError = compiler.Compile(UISchemaURL)
		if compileSchemaError != nil {
			compileSchemaError = fmt.Errorf("编译 UI Schema: %w", compileSchemaError)
			return
		}
		interactionSchema, compileSchemaError = compiler.Compile(InteractionSchemaURL)
		if compileSchemaError != nil {
			compileSchemaError = fmt.Errorf("编译 interaction Schema: %w", compileSchemaError)
		}
	})
	return compileSchemaError
}

func validate(schema *jsonschema.Schema, value any, label string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析 %s JSON: %w", label, err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("%s 不符合 Schema: %w", label, err)
	}
	return nil
}

func ValidateFormSchema(value FormSchema) error {
	if err := schemas(); err != nil {
		return err
	}
	if err := validateFormDocumentLimits(value); err != nil {
		return err
	}
	if err := validate(uiSchema, value, "FormSchema"); err != nil {
		return err
	}
	return validateDataSchema(value.Schema)
}

func ValidateFormData(form FormSchema, value map[string]any) error {
	if err := ValidateFormSchema(form); err != nil {
		return err
	}
	schema, err := compileDataSchema(form.Schema)
	if err != nil {
		return err
	}
	return validate(schema, value, "FormData")
}

func ValidateInteractionRequest(value InteractionRequest) error {
	if err := schemas(); err != nil {
		return err
	}
	if err := validate(interactionSchema, value, "InteractionRequest"); err != nil {
		return err
	}
	if value.Form != nil {
		return ValidateFormSchema(*value.Form)
	}
	return nil
}

const (
	maxFormDocumentBytes = 256 * 1024
	maxFormSchemaDepth   = 32
	maxFormSchemaNodes   = 4096
)

func validateDataSchema(schema JSONSchema) error {
	_, err := compileDataSchema(schema)
	return err
}

func validateFormDocumentLimits(value FormSchema) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("序列化表单文档: %w", err)
	}
	if len(raw) > maxFormDocumentBytes {
		return fmt.Errorf("表单文档超过 %d 字节上限", maxFormDocumentBytes)
	}
	nodes := 0
	document := map[string]any{"schema": map[string]any(value.Schema)}
	if value.UISchema != nil {
		document["uiSchema"] = map[string]any(value.UISchema)
	}
	return inspectSchemaValue(document, 0, &nodes)
}

func compileDataSchema(schema JSONSchema) (*jsonschema.Schema, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("序列化数据 Schema: %w", err)
	}
	if schema["$schema"] != JSONSchemaDialect {
		return nil, fmt.Errorf("数据 Schema 必须显式使用 %s", JSONSchemaDialect)
	}
	if schema["type"] != "object" {
		return nil, fmt.Errorf("数据 Schema 根类型必须是 object")
	}

	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("解析数据 Schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	const resource = "urn:vastplan:form-schema:validation"
	if err := compiler.AddResource(resource, document); err != nil {
		return nil, fmt.Errorf("登记数据 Schema: %w", err)
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		return nil, fmt.Errorf("编译数据 Schema: %w", err)
	}
	return compiled, nil
}

func inspectSchemaValue(value any, depth int, nodes *int) error {
	if depth > maxFormSchemaDepth {
		return fmt.Errorf("表单 Schema 嵌套超过 %d 层上限", maxFormSchemaDepth)
	}
	(*nodes)++
	if *nodes > maxFormSchemaNodes {
		return fmt.Errorf("表单 Schema 节点超过 %d 个上限", maxFormSchemaNodes)
	}
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			if key == "$ref" {
				ref, ok := child.(string)
				if !ok || !strings.HasPrefix(ref, "#") {
					return fmt.Errorf("数据 Schema 只允许本地 $ref")
				}
			}
			if err := inspectSchemaValue(child, depth+1, nodes); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range current {
			if err := inspectSchemaValue(child, depth+1, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}
