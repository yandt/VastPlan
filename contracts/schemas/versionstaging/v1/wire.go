package versionstagingv1

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
	case OperationBeginUpload:
		target = &BeginUploadRequest{}
	case OperationUploadStatus:
		target = &UploadStatusRequest{}
	case OperationRenewUpload:
		target = &RenewUploadRequest{}
	case OperationCompleteUpload, OperationAbortUpload:
		target = &UploadRevisionRequest{}
	default:
		return nil, fmt.Errorf("不支持的 Version Staging 操作 %q", operation)
	}
	if err := decodeStrict(raw, target); err != nil {
		return nil, err
	}
	if err := validateRequest(target); err != nil {
		return nil, err
	}
	return target, nil
}

func ParseResult(operation string, raw []byte) (UploadStatusResult, error) {
	switch operation {
	case OperationBeginUpload, OperationUploadStatus, OperationRenewUpload, OperationCompleteUpload, OperationAbortUpload:
	default:
		return UploadStatusResult{}, fmt.Errorf("不支持的 Version Staging 结果 %q", operation)
	}
	var result UploadStatusResult
	if err := decodeStrict(raw, &result); err != nil {
		return UploadStatusResult{}, err
	}
	if err := ValidateUploadStatusResult(result); err != nil {
		return UploadStatusResult{}, err
	}
	return result, nil
}

func validateRequest(target any) error {
	switch request := target.(type) {
	case *BeginUploadRequest:
		return ValidateBeginUploadRequest(*request)
	case *UploadStatusRequest:
		return ValidateUploadStatusRequest(*request)
	case *RenewUploadRequest:
		return ValidateRenewUploadRequest(*request)
	case *UploadRevisionRequest:
		return ValidateUploadRevisionRequest(*request)
	default:
		return errors.New("Version Staging 请求类型无效")
	}
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Version Staging 只能包含一个 JSON 文档")
	}
	return nil
}
