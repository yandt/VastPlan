package authenticationv1

import (
	"fmt"
	"strings"
	"time"
)

const (
	DeliveryProtocol        = "authentication.delivery.v1"
	DeliveryCapability      = "foundation.security.authentication.delivery"
	OperationDeliver        = "deliver"
	OperationDeliveryHealth = "health"
)

type DeliveryChannel string

const (
	DeliveryEmail DeliveryChannel = "email"
	DeliverySMS   DeliveryChannel = "sms"
)

// DeliveryRequest is an internal, bounded handoff from the OTP Provider to a
// configured delivery service. Code is sensitive and must never be logged.
type DeliveryRequest struct {
	ChallengeID       string          `json:"challengeId"`
	DeliveryProfileID string          `json:"deliveryProfileId"`
	Channel           DeliveryChannel `json:"channel"`
	Identifier        string          `json:"identifier"`
	Locale            string          `json:"locale"`
	Code              string          `json:"code"`
	ExpiresAt         time.Time       `json:"expiresAt"`
}

// DeliveryResult is never returned to the browser. An unresolvable identity
// uses Accepted=false so the OTP Provider can preserve enumeration resistance.
type DeliveryResult struct {
	Accepted  bool   `json:"accepted"`
	SubjectID string `json:"subjectId,omitempty"`
}

type DeliveryHealthRequest struct{}
type DeliveryHealthResult struct {
	Ready bool `json:"ready"`
}

func ParseDeliveryRequest(operation string, raw []byte) (any, error) {
	var target any
	definition := ""
	switch operation {
	case OperationDeliver:
		target, definition = &DeliveryRequest{}, "deliveryRequest"
	case OperationDeliveryHealth:
		target, definition = &DeliveryHealthRequest{}, "healthRequest"
	default:
		return nil, fmt.Errorf("不支持的 Authentication Delivery 操作 %q", operation)
	}
	if err := parseDeliveryMessage(raw, definition, target); err != nil {
		return nil, err
	}
	if request, ok := target.(*DeliveryRequest); ok {
		now := time.Now().UTC()
		if request.ExpiresAt.IsZero() || !request.ExpiresAt.After(now.Add(-5*time.Second)) || request.ExpiresAt.After(now.Add(10*time.Minute)) {
			return nil, fmt.Errorf("Authentication Delivery 挑战过期时间无效")
		}
		if strings.TrimSpace(request.Identifier) != request.Identifier {
			return nil, fmt.Errorf("Authentication Delivery identifier 必须已规范化")
		}
	}
	return target, nil
}

func ParseDeliveryResult(operation string, raw []byte) (any, error) {
	var target any
	definition := ""
	switch operation {
	case OperationDeliver:
		target, definition = &DeliveryResult{}, "deliveryResult"
	case OperationDeliveryHealth:
		target, definition = &DeliveryHealthResult{}, "healthResult"
	default:
		return nil, fmt.Errorf("不支持的 Authentication Delivery 操作 %q", operation)
	}
	if err := parseDeliveryMessage(raw, definition, target); err != nil {
		return nil, err
	}
	if result, ok := target.(*DeliveryResult); ok && result.Accepted != (result.SubjectID != "") {
		return nil, fmt.Errorf("Authentication Delivery accepted 必须与 subjectId 一致")
	}
	return target, nil
}

func parseDeliveryMessage(raw []byte, definition string, target any) error {
	if len(raw) > MaxDeliveryMessageBytes {
		return fmt.Errorf("Authentication Delivery 消息超过 %d bytes", MaxDeliveryMessageBytes)
	}
	if err := validateSchema(DeliverySchemaURL+"#/$defs/"+definition, raw); err != nil {
		return err
	}
	return decodeStrict(raw, target)
}
