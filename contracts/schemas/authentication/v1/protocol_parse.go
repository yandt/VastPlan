package authenticationv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type messageFactory func() any

type operationContract struct {
	requestDefinition string
	requestFactory    messageFactory
	resultDefinition  string
	resultFactory     messageFactory
}

var methodContracts = map[string]operationContract{
	OperationDescribe: contract("describe", func() any { return &DescribeRequest{} }, func() any { return &DescribeResult{} }),
	OperationBegin:    contract("begin", func() any { return &BeginRequest{} }, func() any { return &BeginResult{} }),
	OperationContinue: contract("continue", func() any { return &ContinueRequest{} }, func() any { return &ContinueResult{} }),
	OperationResend:   contract("resend", func() any { return &ResendRequest{} }, func() any { return &ResendResult{} }),
	OperationCancel:   contract("cancel", func() any { return &CancelRequest{} }, func() any { return &CancelResult{} }),
	OperationHealth:   contract("health", func() any { return &HealthRequest{} }, func() any { return &HealthResult{} }),
}

func contract(name string, request, result messageFactory) operationContract {
	return operationContract{requestDefinition: name + "Request", requestFactory: request, resultDefinition: name + "Result", resultFactory: result}
}

func ParseMethodRequest(operation string, raw []byte) (any, error) {
	return parseMethodMessage(operation, raw, true)
}

func ParseMethodResult(operation string, raw []byte) (any, error) {
	return parseMethodMessage(operation, raw, false)
}

func parseMethodMessage(operation string, raw []byte, request bool) (any, error) {
	definition, exists := methodContracts[operation]
	if !exists {
		return nil, fmt.Errorf("不支持的 Authentication Method 操作 %q", operation)
	}
	if len(raw) > MaxMethodMessageBytes {
		return nil, fmt.Errorf("Authentication Method 消息超过 %d bytes", MaxMethodMessageBytes)
	}
	name, factory := definition.resultDefinition, definition.resultFactory
	if request {
		name, factory = definition.requestDefinition, definition.requestFactory
	}
	if err := validateSchema(MethodSchemaURL+"#/$defs/"+name, raw); err != nil {
		return nil, err
	}
	target := factory()
	if err := decodeStrict(raw, target); err != nil {
		return nil, err
	}
	if err := validateMethodMessage(target); err != nil {
		return nil, err
	}
	return target, nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Authentication 消息只能包含一个 JSON 文档")
	}
	return nil
}
