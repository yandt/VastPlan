package versioningv1

import (
	"encoding/json"
	"errors"
	"strings"
)

func ValidateComparisonResult(result CompareVersionsResult) error {
	if err := validateVersionPair(result.Left, result.Right); err != nil {
		return err
	}
	if len(result.Patch) > MaxPatchOperations || result.Summary.Total != len(result.Patch) || result.Summary.Total != result.Summary.Added+result.Summary.Removed+result.Summary.Replaced {
		return errors.New("版本比较统计无效")
	}
	counts := ChangeSummary{}
	for _, operation := range result.Patch {
		if operation.Path != "" && !strings.HasPrefix(operation.Path, "/") {
			return errors.New("JSON Patch path 无效")
		}
		switch operation.Operation {
		case "add":
			counts.Added++
			if !validPatchValue(operation.Value) {
				return errors.New("JSON Patch add 缺少有效 value")
			}
		case "remove":
			counts.Removed++
			if len(operation.Value) != 0 {
				return errors.New("JSON Patch remove 不得携带 value")
			}
		case "replace":
			counts.Replaced++
			if !validPatchValue(operation.Value) {
				return errors.New("JSON Patch replace 缺少有效 value")
			}
		default:
			return errors.New("JSON Patch operation 无效")
		}
	}
	if counts.Added != result.Summary.Added || counts.Removed != result.Summary.Removed || counts.Replaced != result.Summary.Replaced {
		return errors.New("JSON Patch operation 与统计不一致")
	}
	return nil
}

func validPatchValue(value json.RawMessage) bool {
	return len(value) > 0 && json.Valid(value)
}
