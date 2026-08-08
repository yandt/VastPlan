// Package workflowv1 defines the language-neutral workflow orchestration and
// governed domain-action contracts.
package workflowv1

import (
	"encoding/json"
	"time"
)

const (
	OrchestrationProtocol    = "workflow.orchestration.v1"
	ActionProtocol           = "workflow.action.v1"
	NodeEffectProtocol       = "workflow.node-effect.v1"
	NodeTemplateProtocol     = "workflow.node-template.v1"
	OrchestratorPluginID     = "cn.vastplan.platform.workflow.orchestrator"
	OrchestrationCapability  = "platform.workflow.orchestrator"
	FeatureContributionKind  = "backend.workflowFeatures"
	TemplateContributionKind = "backend.workflowNodeTemplates"
	ProviderContributionKind = "backend.workflowNodeProviders"
)

type NodeTypeID string

const (
	NodeManual       NodeTypeID = "workflow.core.manual-task"
	NodeAction       NodeTypeID = "workflow.core.action"
	NodeEnd          NodeTypeID = "workflow.core.end"
	CoreNodeContract            = "1.0.0"
)

type NodeTypeRef struct {
	ID       NodeTypeID `json:"id"`
	Contract string     `json:"contract"`
}

func CoreNode(nodeType NodeTypeID) NodeTypeRef {
	return NodeTypeRef{ID: nodeType, Contract: CoreNodeContract}
}

type EndResult string

const (
	ResultSucceeded EndResult = "succeeded"
	ResultRejected  EndResult = "rejected"
	ResultCancelled EndResult = "cancelled"
)

type InstanceStatus string

const (
	InstanceRunning   InstanceStatus = "running"
	InstanceSucceeded InstanceStatus = "succeeded"
	InstanceRejected  InstanceStatus = "rejected"
	InstanceCancelled InstanceStatus = "cancelled"
	InstanceSuspended InstanceStatus = "suspended"
)

type WorkStatus string

const (
	WorkPending   WorkStatus = "pending"
	WorkCompleted WorkStatus = "completed"
)

type UnboundPolicy string

const (
	UnboundDeny   UnboundPolicy = "deny"
	UnboundDirect UnboundPolicy = "direct"
)

type ExecutionMode string

const (
	ExecutionWorkflow ExecutionMode = "workflow"
	ExecutionDirect   ExecutionMode = "direct"
)

// FeatureDescriptor is signed by the feature-owning plugin. Action routing is
// resolved from this descriptor and cannot be overridden by workflow input.
type FeatureDescriptor struct {
	ID              string               `json:"id"`
	Contract        string               `json:"contract"`
	ResourceKind    string               `json:"resourceKind"`
	DigestRequired  bool                 `json:"digestRequired"`
	Prepare         *OperationDescriptor `json:"prepare,omitempty"`
	UnboundPolicy   UnboundPolicy        `json:"unboundPolicy,omitempty"`
	UnboundActionID string               `json:"unboundActionId,omitempty"`
	Actions         []ActionDescriptor   `json:"actions"`
}

type OperationDescriptor struct {
	Capability string `json:"capability"`
	Operation  string `json:"operation"`
	Permission string `json:"permission"`
}

type ActionDescriptor struct {
	ID            string `json:"id"`
	Capability    string `json:"capability"`
	Operation     string `json:"operation"`
	Permission    string `json:"permission"`
	Terminal      bool   `json:"terminal"`
	Compensatable bool   `json:"compensatable"`
}

type Definition struct {
	ID          string `json:"id"`
	Revision    int64  `json:"revision"`
	FeatureID   string `json:"featureId"`
	EntryNodeID string `json:"entryNodeId"`
	Nodes       []Node `json:"nodes"`
}

type Node struct {
	ID          string            `json:"id"`
	Type        NodeTypeRef       `json:"type"`
	Title       string            `json:"title,omitempty"`
	Roles       []string          `json:"roles,omitempty"`
	Outcomes    map[string]string `json:"outcomes,omitempty"`
	ActionID    string            `json:"actionId,omitempty"`
	Next        string            `json:"next,omitempty"`
	Result      EndResult         `json:"result,omitempty"`
	Config      json.RawMessage   `json:"config,omitempty"`
	Transitions map[string]string `json:"transitions,omitempty"`
}

type DefinitionRef struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	Digest   string `json:"digest"`
}

type Binding struct {
	ServiceID  string        `json:"serviceId"`
	FeatureID  string        `json:"featureId"`
	Definition DefinitionRef `json:"definition"`
	Revision   int64         `json:"revision"`
	UpdatedAt  time.Time     `json:"updatedAt"`
	UpdatedBy  string        `json:"updatedBy"`
}

type ResourceRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type StartRequest struct {
	ID             string          `json:"id"`
	ServiceID      string          `json:"serviceId"`
	FeatureID      string          `json:"featureId"`
	Resource       ResourceRef     `json:"resource"`
	ResourceDigest string          `json:"resourceDigest,omitempty"`
	Facts          json.RawMessage `json:"facts,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey"`
}

type Instance struct {
	ID             string         `json:"id"`
	ServiceID      string         `json:"serviceId"`
	FeatureID      string         `json:"featureId"`
	Definition     DefinitionRef  `json:"definition"`
	Resource       ResourceRef    `json:"resource"`
	ResourceDigest string         `json:"resourceDigest,omitempty"`
	Mode           ExecutionMode  `json:"mode"`
	Status         InstanceStatus `json:"status"`
	CurrentNodeID  string         `json:"currentNodeId,omitempty"`
	Revision       int64          `json:"revision"`
	StartedBy      string         `json:"startedBy"`
	StartedAt      time.Time      `json:"startedAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
	Audit          []AuditEvent   `json:"audit"`
}

type Task struct {
	ID               string     `json:"id"`
	InstanceID       string     `json:"instanceId"`
	NodeID           string     `json:"nodeId"`
	Title            string     `json:"title"`
	Roles            []string   `json:"roles,omitempty"`
	AllowedOutcomes  []string   `json:"allowedOutcomes"`
	Status           WorkStatus `json:"status"`
	Revision         int64      `json:"revision"`
	CreatedAt        time.Time  `json:"createdAt"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
	CompletedBy      string     `json:"completedBy,omitempty"`
	CompletedOutcome string     `json:"completedOutcome,omitempty"`
}

type ActionWork struct {
	ID             string          `json:"id"`
	InstanceID     string          `json:"instanceId"`
	NodeID         string          `json:"nodeId"`
	ActionID       string          `json:"actionId"`
	Attempt        int64           `json:"attempt"`
	IdempotencyKey string          `json:"idempotencyKey"`
	Status         WorkStatus      `json:"status"`
	Revision       int64           `json:"revision"`
	CreatedAt      time.Time       `json:"createdAt"`
	CompletedAt    *time.Time      `json:"completedAt,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
}

type CompleteTaskRequest struct {
	TaskID           string `json:"taskId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Outcome          string `json:"outcome"`
	Comment          string `json:"comment,omitempty"`
}

type CompleteActionRequest struct {
	WorkID           string          `json:"workId"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Succeeded        bool            `json:"succeeded"`
	Result           json.RawMessage `json:"result,omitempty"`
	ErrorCode        string          `json:"errorCode,omitempty"`
}

type CancelRequest struct {
	InstanceID       string `json:"instanceId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

type ActionRequest struct {
	InstanceID     string        `json:"instanceId"`
	Definition     DefinitionRef `json:"definition"`
	FeatureID      string        `json:"featureId"`
	ActionID       string        `json:"actionId"`
	Resource       ResourceRef   `json:"resource"`
	ResourceDigest string        `json:"resourceDigest,omitempty"`
	Attempt        int64         `json:"attempt"`
	IdempotencyKey string        `json:"idempotencyKey"`
}

type AuditEvent struct {
	Action  string    `json:"action"`
	NodeID  string    `json:"nodeId,omitempty"`
	ActorID string    `json:"actorId"`
	Outcome string    `json:"outcome,omitempty"`
	At      time.Time `json:"at"`
}
