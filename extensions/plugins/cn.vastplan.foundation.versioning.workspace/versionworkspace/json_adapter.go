package versionworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

type JSONAdapter struct{}

func NewJSONAdapter() *JSONAdapter { return &JSONAdapter{} }

func (*JSONAdapter) Descriptor() resourcev1.AdapterDescriptor {
	return resourcev1.AdapterDescriptor{
		Protocol: resourcev1.Protocol, ID: JSONAdapterID, Version: "1.0.0",
		ContentKind: resourcev1.ContentJSON, SupportedModes: []string{resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot,
		MaxSnapshotBytes: 1 << 20, SecretPolicy: resourcev1.SecretPolicyCredentialRefsOnly,
		Capabilities:        resourcev1.AdapterCapabilities{Normalize: true, Diff: true},
		ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
	}
}

func (a *JSONAdapter) Normalize(ctx context.Context, request resourcev1.AdapterNormalizeRequest) (resourcev1.AdapterNormalizeResult, error) {
	if err := ctx.Err(); err != nil {
		return resourcev1.AdapterNormalizeResult{}, err
	}
	descriptor := a.Descriptor()
	if err := resourcev1.ValidateAdapterNormalizeRequest(request, descriptor.MaxSnapshotBytes); err != nil {
		return resourcev1.AdapterNormalizeResult{}, err
	}
	if request.Mode != resourcev1.ModeSnapshot || request.Snapshot.Kind != resourcev1.ContentJSON {
		return resourcev1.AdapterNormalizeResult{}, errors.New("JSON Adapter 只支持 snapshot JSON")
	}
	if err := validateEmptyAdapterConfiguration(request.Configuration); err != nil {
		return resourcev1.AdapterNormalizeResult{}, err
	}
	var value map[string]any
	if err := json.Unmarshal(request.Snapshot.JSON, &value); err != nil {
		return resourcev1.AdapterNormalizeResult{}, err
	}
	if path := plaintextSecretPath(value, ""); path != "" {
		return resourcev1.AdapterNormalizeResult{}, fmt.Errorf("JSON Snapshot 的 %s 疑似包含秘密明文；只能保存 ManagedCredentialRef", path)
	}
	canonical, err := resourcev1.CanonicalContent(request.Snapshot, descriptor.MaxSnapshotBytes)
	if err != nil {
		return resourcev1.AdapterNormalizeResult{}, err
	}
	normalized := resourcev1.Snapshot{Kind: resourcev1.ContentJSON, MediaType: "application/json", JSON: canonical}
	digest, err := resourcev1.SnapshotDigest(normalized, descriptor.MaxSnapshotBytes)
	if err != nil {
		return resourcev1.AdapterNormalizeResult{}, err
	}
	return resourcev1.AdapterNormalizeResult{Snapshot: normalized, Digest: digest}, nil
}

func (a *JSONAdapter) Diff(ctx context.Context, request resourcev1.AdapterDiffRequest) (resourcev1.AdapterDiffResult, error) {
	if err := ctx.Err(); err != nil {
		return resourcev1.AdapterDiffResult{}, err
	}
	if err := resourcev1.ValidateAdapterDiffRequest(request, a.Descriptor().MaxSnapshotBytes); err != nil {
		return resourcev1.AdapterDiffResult{}, err
	}
	if request.Mode != resourcev1.ModeSnapshot || request.Left.Kind != resourcev1.ContentJSON {
		return resourcev1.AdapterDiffResult{}, errors.New("JSON Adapter 只支持 snapshot JSON diff")
	}
	var left, right any
	if err := json.Unmarshal(request.Left.JSON, &left); err != nil {
		return resourcev1.AdapterDiffResult{}, err
	}
	if err := json.Unmarshal(request.Right.JSON, &right); err != nil {
		return resourcev1.AdapterDiffResult{}, err
	}
	changes := make([]jsonChange, 0)
	collectJSONChanges(left, right, "", &changes)
	if len(changes) > resourcev1.MaxChangedPaths {
		return resourcev1.AdapterDiffResult{}, errors.New("JSON Snapshot 变化路径超过限制")
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].path < changes[j].path })
	result := resourcev1.AdapterDiffResult{ChangedPaths: make([]string, 0, len(changes))}
	for _, change := range changes {
		result.ChangedPaths = append(result.ChangedPaths, change.path)
		switch change.kind {
		case changeAdded:
			result.Summary.Added++
		case changeRemoved:
			result.Summary.Removed++
		default:
			result.Summary.Modified++
		}
	}
	result.Summary.Total = len(changes)
	return result, resourcev1.ValidateAdapterDiffResult(result)
}

func validateEmptyAdapterConfiguration(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil || len(value) != 0 {
		return errors.New("JSON Adapter 当前不接受自定义配置")
	}
	return nil
}

var sensitiveJSONKeys = map[string]struct{}{
	"password": {}, "passwd": {}, "secret": {}, "token": {}, "credential": {}, "credentials": {},
	"credentialref": {}, "privatekey": {}, "apikey": {}, "accesskey": {}, "clientkey": {}, "clientsecret": {}, "sslkey": {},
}

func plaintextSecretPath(value any, current string) string {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := current + "/" + escapeJSONPointer(key)
			normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
			if _, sensitive := sensitiveJSONKeys[normalized]; sensitive && !containsOnlyCredentialRefs(typed[key]) {
				return childPath
			}
			if nested := plaintextSecretPath(typed[key], childPath); nested != "" {
				return nested
			}
		}
	case []any:
		for index, child := range typed {
			if nested := plaintextSecretPath(child, current+"/"+strconv.Itoa(index)); nested != "" {
				return nested
			}
		}
	}
	return ""
}

func containsOnlyCredentialRefs(value any) bool {
	if ref, ok := decodeCredentialRef(value); ok {
		return commonv1.ValidateManagedCredentialRef(ref) == nil
	}
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			return false
		}
		for _, child := range typed {
			if !containsOnlyCredentialRefs(child) {
				return false
			}
		}
		return true
	case []any:
		if len(typed) == 0 {
			return false
		}
		for _, child := range typed {
			if !containsOnlyCredentialRefs(child) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func decodeCredentialRef(value any) (commonv1.ManagedCredentialRef, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return commonv1.ManagedCredentialRef{}, false
	}
	var ref commonv1.ManagedCredentialRef
	if err := json.Unmarshal(raw, &ref); err != nil || ref.Handle == "" {
		return commonv1.ManagedCredentialRef{}, false
	}
	return ref, true
}

type changeKind uint8

const (
	changeModified changeKind = iota
	changeAdded
	changeRemoved
)

type jsonChange struct {
	path string
	kind changeKind
}

func collectJSONChanges(left, right any, pointer string, changes *[]jsonChange) {
	leftObject, leftIsObject := left.(map[string]any)
	rightObject, rightIsObject := right.(map[string]any)
	if leftIsObject && rightIsObject {
		keys := make(map[string]struct{}, len(leftObject)+len(rightObject))
		for key := range leftObject {
			keys[key] = struct{}{}
		}
		for key := range rightObject {
			keys[key] = struct{}{}
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			leftValue, leftExists := leftObject[key]
			rightValue, rightExists := rightObject[key]
			path := pointer + "/" + escapeJSONPointer(key)
			switch {
			case !leftExists:
				*changes = append(*changes, jsonChange{path: path, kind: changeAdded})
			case !rightExists:
				*changes = append(*changes, jsonChange{path: path, kind: changeRemoved})
			default:
				collectJSONChanges(leftValue, rightValue, path, changes)
			}
		}
		return
	}
	leftArray, leftIsArray := left.([]any)
	rightArray, rightIsArray := right.([]any)
	if leftIsArray && rightIsArray {
		shared := len(leftArray)
		if len(rightArray) < shared {
			shared = len(rightArray)
		}
		for index := 0; index < shared; index++ {
			collectJSONChanges(leftArray[index], rightArray[index], pointer+"/"+strconv.Itoa(index), changes)
		}
		for index := shared; index < len(leftArray); index++ {
			*changes = append(*changes, jsonChange{path: pointer + "/" + strconv.Itoa(index), kind: changeRemoved})
		}
		for index := shared; index < len(rightArray); index++ {
			*changes = append(*changes, jsonChange{path: pointer + "/" + strconv.Itoa(index), kind: changeAdded})
		}
		return
	}
	if !reflect.DeepEqual(left, right) {
		if pointer == "" {
			pointer = "/"
		}
		*changes = append(*changes, jsonChange{path: pointer, kind: changeModified})
	}
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
