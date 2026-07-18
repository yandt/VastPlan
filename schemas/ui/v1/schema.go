// Package uiv1 defines the serializable UI and interaction contracts shared by
// Web, Mobile, Runner and Backend. It deliberately contains no renderer API.
package uiv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	UIContractVersion          = "1.0.0"
	InteractionContractVersion = "1.0.0"
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

type FormFieldType string

const (
	FieldText        FormFieldType = "text"
	FieldTextarea    FormFieldType = "textarea"
	FieldNumber      FormFieldType = "number"
	FieldBoolean     FormFieldType = "boolean"
	FieldSelect      FormFieldType = "select"
	FieldMultiSelect FormFieldType = "multiSelect"
	FieldDate        FormFieldType = "date"
	FieldObject      FormFieldType = "object"
	FieldArray       FormFieldType = "array"
	FieldSecretRef   FormFieldType = "secretRef"
)

type FormCondition struct {
	Key       string `json:"key"`
	Equals    any    `json:"equals,omitempty"`
	NotEquals any    `json:"notEquals,omitempty"`
}

type FormValidation struct {
	Required bool     `json:"required,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Pattern  string   `json:"pattern,omitempty"`
	Message  string   `json:"message,omitempty"`
}

type FormOption struct {
	Label    string `json:"label"`
	Value    any    `json:"value"`
	Disabled bool   `json:"disabled,omitempty"`
}

type FormField struct {
	Key          string          `json:"key"`
	Type         FormFieldType   `json:"type"`
	Title        string          `json:"title"`
	Help         string          `json:"help,omitempty"`
	DefaultValue any             `json:"defaultValue,omitempty"`
	Options      []FormOption    `json:"options,omitempty"`
	Validation   *FormValidation `json:"validation,omitempty"`
	VisibleWhen  *FormCondition  `json:"visibleWhen,omitempty"`
	ReadOnly     bool            `json:"readOnly,omitempty"`
	Disabled     bool            `json:"disabled,omitempty"`
	Fields       []FormField     `json:"fields,omitempty"`
}

type FormSchema struct {
	ID     string      `json:"id"`
	Title  string      `json:"title,omitempty"`
	Fields []FormField `json:"fields"`
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
	return validate(uiSchema, value, "FormSchema")
}

func ValidateInteractionRequest(value InteractionRequest) error {
	if err := schemas(); err != nil {
		return err
	}
	return validate(interactionSchema, value, "InteractionRequest")
}
