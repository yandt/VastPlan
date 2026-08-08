package workflowv1

import "encoding/json"

const GovernedOperationProtocol = "governed.operation.v1"

type PreparedResource struct {
	Resource   ResourceRef     `json:"resource"`
	Digest     string          `json:"digest"`
	Revision   int64           `json:"revision"`
	Projection json.RawMessage `json:"projection,omitempty"`
}

type GovernedOperationRequest struct {
	ServiceID      string          `json:"serviceId"`
	FeatureID      string          `json:"featureId"`
	PreparePayload json.RawMessage `json:"preparePayload"`
	IdempotencyKey string          `json:"idempotencyKey"`
}

type GovernedOperationResult struct {
	Resource PreparedResource `json:"resource"`
	Instance Instance         `json:"instance"`
}
