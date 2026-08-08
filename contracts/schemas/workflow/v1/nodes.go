package workflowv1

import (
	"encoding/json"
	"time"
)

type ArtifactDocumentRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// NodeTemplateDescriptor identifies a signed, declarative expansion document.
// The template is compiled to Core nodes when a definition is published.
type NodeTemplateDescriptor struct {
	ID                  string              `json:"id"`
	Contract            string              `json:"contract"`
	Title               string              `json:"title"`
	CompilerContract    string              `json:"compilerContract"`
	ConfigurationSchema ArtifactDocumentRef `json:"configurationSchema"`
	Expansion           ArtifactDocumentRef `json:"expansion"`
	Outcomes            []string            `json:"outcomes"`
}

// NodeProviderDescriptor binds an exceptional runtime node to one signed
// operation. Definition input cannot override this target.
type NodeProviderDescriptor struct {
	ID                  string               `json:"id"`
	Contract            string               `json:"contract"`
	Title               string               `json:"title"`
	EffectContract      string               `json:"effectContract"`
	ConfigurationSchema ArtifactDocumentRef  `json:"configurationSchema"`
	InputSchema         *ArtifactDocumentRef `json:"inputSchema,omitempty"`
	OutputSchema        *ArtifactDocumentRef `json:"outputSchema,omitempty"`
	Capability          string               `json:"capability"`
	Operation           string               `json:"operation"`
	Permission          string               `json:"permission"`
	Outcomes            []string             `json:"outcomes"`
}

type NodeSignal struct {
	Contract      string          `json:"contract"`
	CorrelationID string          `json:"correlationId"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

type NodeInvocation struct {
	InstanceID     string          `json:"instanceId"`
	NodeID         string          `json:"nodeId"`
	Attempt        int64           `json:"attempt"`
	Configuration  json.RawMessage `json:"configuration"`
	Facts          json.RawMessage `json:"facts,omitempty"`
	Signal         *NodeSignal     `json:"signal,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey"`
}

type NodeEffectKind string

const (
	NodeEffectComplete   NodeEffectKind = "complete"
	NodeEffectWait       NodeEffectKind = "wait"
	NodeEffectRetryAfter NodeEffectKind = "retry-after"
	NodeEffectFail       NodeEffectKind = "fail"
)

type WaitEffect struct {
	EventContract string    `json:"eventContract"`
	CorrelationID string    `json:"correlationId"`
	Deadline      time.Time `json:"deadline"`
}

type NodeEffect struct {
	Kind      NodeEffectKind  `json:"kind"`
	Outcome   string          `json:"outcome,omitempty"`
	Facts     json.RawMessage `json:"facts,omitempty"`
	Wait      *WaitEffect     `json:"wait,omitempty"`
	RetryAt   *time.Time      `json:"retryAt,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	ErrorCode string          `json:"errorCode,omitempty"`
}
