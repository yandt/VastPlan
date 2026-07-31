package versioncontentv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func ParseRequest(operation string, raw []byte) (any, error) {
	var target any
	switch operation {
	case OperationPrepare:
		target = &PrepareRequest{}
	case OperationStatus:
		target = &StatusRequest{}
	case OperationConfirm:
		target = &ConfirmRequest{}
	case OperationAbort:
		target = &AbortRequest{}
	default:
		return nil, fmt.Errorf("不支持的 Version Content 操作 %q", operation)
	}
	if err := decodeStrict(raw, target); err != nil {
		return nil, err
	}
	var err error
	switch request := target.(type) {
	case *PrepareRequest:
		err = ValidatePrepareRequest(*request)
	case *StatusRequest:
		err = ValidateStatusRequest(*request)
	case *ConfirmRequest:
		err = ValidateConfirmRequest(*request)
	case *AbortRequest:
		err = ValidateAbortRequest(*request)
	}
	return target, err
}

func ParseResult(operation string, raw []byte) (ProtectionResult, error) {
	switch operation {
	case OperationPrepare, OperationStatus, OperationConfirm, OperationAbort:
	default:
		return ProtectionResult{}, fmt.Errorf("不支持的 Version Content 结果 %q", operation)
	}
	var result ProtectionResult
	if err := decodeStrict(raw, &result); err != nil {
		return ProtectionResult{}, err
	}
	if err := ValidateProtection(result.Protection); err != nil {
		return ProtectionResult{}, err
	}
	return result, nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Version Content 只能包含一个 JSON 文档")
	}
	return nil
}
