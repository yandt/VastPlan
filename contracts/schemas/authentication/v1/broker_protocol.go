package authenticationv1

import (
	"encoding/json"
	"errors"
)

// BrokerContinueResult is distinct from a Provider ContinueResult: only the
// trusted Broker may add a signed platform assertion after validating Evidence
// against the server-owned transaction route.
type BrokerContinueResult struct {
	Result    MethodResult                   `json:"result"`
	Assertion *SignedAuthenticationAssertion `json:"assertion,omitempty"`
}

func ParseBrokerContinueResult(raw []byte) (BrokerContinueResult, error) {
	if len(raw) > MaxMethodMessageBytes+MaxAssertionBytes {
		return BrokerContinueResult{}, errors.New("Authentication Broker 响应超过大小上限")
	}
	if err := validateSchema(BrokerSchemaURL, raw); err != nil {
		return BrokerContinueResult{}, err
	}
	var value BrokerContinueResult
	if err := decodeStrict(raw, &value); err != nil {
		return BrokerContinueResult{}, err
	}
	methodRaw, _ := json.Marshal(ContinueResult{Result: value.Result})
	if _, err := ParseMethodResult(OperationContinue, methodRaw); err != nil {
		return BrokerContinueResult{}, err
	}
	if value.Result.State == StateAuthenticated {
		if value.Assertion == nil {
			return BrokerContinueResult{}, errors.New("Broker authenticated 响应缺少签名 Assertion")
		}
		assertionRaw, _ := json.Marshal(value.Assertion)
		parsed, err := ParseSignedAssertion(assertionRaw)
		if err != nil {
			return BrokerContinueResult{}, err
		}
		evidence := value.Result.Evidence
		if evidence == nil || parsed.Payload.TransactionID != evidence.TransactionID || parsed.Payload.ProviderID != evidence.ProviderID || parsed.Payload.Subject != evidence.Subject {
			return BrokerContinueResult{}, errors.New("Broker Assertion 与 Provider Evidence 不一致")
		}
	} else if value.Assertion != nil {
		return BrokerContinueResult{}, errors.New("非 authenticated Broker 响应不得携带 Assertion")
	}
	return value, nil
}
