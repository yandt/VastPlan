package versionledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func (s *Service) compareVersions(ctx context.Context, scope Scope, request versioningv1.CompareVersionsRequest) (versioningv1.CompareVersionsResult, error) {
	provider, err := s.provider(request.Left.Stream.Namespace)
	if err != nil {
		return versioningv1.CompareVersionsResult{}, err
	}
	left, err := exactProviderVersion(ctx, provider, scope, request.Left)
	if err != nil {
		return versioningv1.CompareVersionsResult{}, err
	}
	right, err := exactProviderVersion(ctx, provider, scope, request.Right)
	if err != nil {
		return versioningv1.CompareVersionsResult{}, err
	}
	leftValue, err := decodeContent(left.Content)
	if err != nil {
		return versioningv1.CompareVersionsResult{}, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	rightValue, err := decodeContent(right.Content)
	if err != nil {
		return versioningv1.CompareVersionsResult{}, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	patch := make([]versioningv1.JSONPatchOperation, 0)
	if err := appendJSONDiff(&patch, "", leftValue, rightValue); err != nil {
		return versioningv1.CompareVersionsResult{}, err
	}
	summary := versioningv1.ChangeSummary{Total: len(patch)}
	for _, operation := range patch {
		switch operation.Operation {
		case "add":
			summary.Added++
		case "remove":
			summary.Removed++
		case "replace":
			summary.Replaced++
		}
	}
	return versioningv1.CompareVersionsResult{Left: request.Left, Right: request.Right, Patch: patch, Summary: summary}, nil
}

func exactProviderVersion(ctx context.Context, provider Provider, scope Scope, ref versioningv1.VersionRef) (versioningv1.VersionRecord, error) {
	result, err := provider.GetVersion(ctx, scope, versioningv1.GetVersionRequest{Ref: ref})
	if err != nil {
		return versioningv1.VersionRecord{}, err
	}
	if result.Version.Ref != ref || versioningv1.ValidateVersionRecord(result.Version) != nil {
		return versioningv1.VersionRecord{}, providerError(versioningv1.ErrorCorrupted, false, errors.New("Provider 返回了错误的版本记录"))
	}
	return result.Version, nil
}

func decodeContent(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func appendJSONDiff(patch *[]versioningv1.JSONPatchOperation, path string, left, right any) error {
	if reflect.DeepEqual(left, right) {
		return nil
	}
	leftObject, leftIsObject := left.(map[string]any)
	rightObject, rightIsObject := right.(map[string]any)
	if leftIsObject && rightIsObject {
		keys := make([]string, 0, len(leftObject)+len(rightObject))
		seen := map[string]struct{}{}
		for key := range leftObject {
			seen[key], keys = struct{}{}, append(keys, key)
		}
		for key := range rightObject {
			if _, exists := seen[key]; !exists {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			leftChild, leftExists := leftObject[key]
			rightChild, rightExists := rightObject[key]
			childPath := path + "/" + escapeJSONPointer(key)
			switch {
			case !rightExists:
				if err := appendPatch(patch, versioningv1.JSONPatchOperation{Operation: "remove", Path: childPath}); err != nil {
					return err
				}
			case !leftExists:
				value, err := json.Marshal(rightChild)
				if err != nil {
					return err
				}
				if err := appendPatch(patch, versioningv1.JSONPatchOperation{Operation: "add", Path: childPath, Value: value}); err != nil {
					return err
				}
			default:
				if err := appendJSONDiff(patch, childPath, leftChild, rightChild); err != nil {
					return err
				}
			}
		}
		return nil
	}
	value, err := json.Marshal(right)
	if err != nil {
		return err
	}
	return appendPatch(patch, versioningv1.JSONPatchOperation{Operation: "replace", Path: path, Value: value})
}

func appendPatch(patch *[]versioningv1.JSONPatchOperation, operation versioningv1.JSONPatchOperation) error {
	if len(*patch) == versioningv1.MaxPatchOperations {
		return providerError(versioningv1.ErrorLimitExceeded, false, errors.New("JSON Patch operation 超过限制"))
	}
	*patch = append(*patch, operation)
	return nil
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
