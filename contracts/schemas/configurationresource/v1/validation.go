package configurationresourcev1

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const maxValuesBytes = 64 << 10

//go:embed vastplan.configuration-resource-controller.schema.json
var schemaJSON []byte

var (
	schemaOnce sync.Once
	schemas    map[string]*jsonschema.Schema
	schemaErr  error
)

var requestDefinitions = map[string]string{
	OperationList: "listRequest", OperationGet: "getRequest", OperationPrepare: "prepareRequest",
	OperationCommit: "candidateRequest", OperationAbort: "candidateRequest", OperationStatus: "statusRequest",
}

func compileSchemas() {
	compiler := jsonschema.NewCompiler()
	if err := commonv1.AddResources(compiler); err != nil {
		schemaErr = err
		return
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		schemaErr = fmt.Errorf("解析 Configuration Resource Schema: %w", err)
		return
	}
	if err := compiler.AddResource(SchemaURL, document); err != nil {
		schemaErr = fmt.Errorf("登记 Configuration Resource Schema: %w", err)
		return
	}
	schemas = map[string]*jsonschema.Schema{}
	for _, definition := range []string{"listRequest", "getRequest", "prepareRequest", "candidateRequest", "statusRequest", "listResponse", "getResponse", "observation"} {
		compiled, compileErr := compiler.Compile(SchemaURL + "#/$defs/" + definition)
		if compileErr != nil {
			schemaErr = fmt.Errorf("编译 Configuration Resource Schema %s: %w", definition, compileErr)
			return
		}
		schemas[definition] = compiled
	}
}

func validateDefinition(definition string, raw []byte) error {
	schemaOnce.Do(compileSchemas)
	if schemaErr != nil {
		return schemaErr
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析 Configuration Resource JSON: %w", err)
	}
	if err := schemas[definition].Validate(instance); err != nil {
		return fmt.Errorf("Configuration Resource %s 不符合 Schema: %w", definition, err)
	}
	return nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Configuration Resource 只能包含一个 JSON 文档")
	}
	return nil
}

func ParseRequest(operation string, raw []byte) (any, error) {
	definition, ok := requestDefinitions[operation]
	if !ok {
		return nil, fmt.Errorf("不支持的 Configuration Resource 操作 %q", operation)
	}
	if err := validateDefinition(definition, raw); err != nil {
		return nil, err
	}
	var target any
	switch operation {
	case OperationList:
		target = &ListRequest{}
	case OperationGet:
		target = &GetRequest{}
	case OperationPrepare:
		target = &PrepareRequest{}
	case OperationCommit, OperationAbort:
		target = &CandidateRequest{}
	case OperationStatus:
		target = &StatusRequest{}
	}
	if err := decodeStrict(raw, target); err != nil {
		return nil, err
	}
	switch request := target.(type) {
	case *PrepareRequest:
		normalized, err := NormalizePrepareRequest(*request)
		if err != nil {
			return nil, err
		}
		*request = normalized
	case *StatusRequest:
		if (request.CandidateID == "") != (request.RequestDigest == "") {
			return nil, errors.New("status 的 candidateId 与 requestDigest 必须同时提供")
		}
	}
	return target, nil
}

func NormalizePrepareRequest(request PrepareRequest) (PrepareRequest, error) {
	hasValues := len(request.Values) > 0
	switch request.Action {
	case ActionCreate:
		if request.ExpectedActive != nil || !hasValues {
			return PrepareRequest{}, errors.New("create 必须提供 values 且不得提供 expectedActive")
		}
	case ActionUpdate:
		if request.ExpectedActive == nil || !hasValues {
			return PrepareRequest{}, errors.New("update 必须提供 expectedActive 与 values")
		}
	case ActionDelete:
		if request.ExpectedActive == nil || hasValues || len(request.ManagedCredentials) > 0 {
			return PrepareRequest{}, errors.New("delete 只接受 expectedActive，不接受 values 或新凭证")
		}
	default:
		return PrepareRequest{}, errors.New("Configuration Resource action 无效")
	}
	if hasValues {
		if len(request.Values) > maxValuesBytes {
			return PrepareRequest{}, errors.New("Configuration Resource values 大小无效")
		}
		var values map[string]any
		if err := json.Unmarshal(request.Values, &values); err != nil || values == nil {
			return PrepareRequest{}, errors.New("Configuration Resource values 必须是 JSON 对象")
		}
		request.Values, _ = json.Marshal(values)
	}
	if len(request.ManagedCredentials) > 64 {
		return PrepareRequest{}, errors.New("Configuration Resource 托管凭证数量超限")
	}
	credentials := make(map[string]commonv1.ManagedCredentialRef, len(request.ManagedCredentials))
	for fieldID, ref := range request.ManagedCredentials {
		if strings.TrimSpace(fieldID) == "" || !strings.HasPrefix(ref.Handle, "credential://managed/") || ref.Scope != "tenant" ||
			strings.TrimSpace(ref.Owner) == "" || strings.TrimSpace(ref.Purpose) == "" || ref.Version < 1 {
			return PrepareRequest{}, errors.New("Configuration Resource 包含无效托管凭证引用")
		}
		credentials[fieldID] = ref
	}
	if len(credentials) == 0 {
		request.ManagedCredentials = nil
	} else {
		request.ManagedCredentials = credentials
	}
	return request, nil
}

func DigestPrepareRequest(request PrepareRequest) (string, error) {
	normalized, err := NormalizePrepareRequest(request)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	if err := validateDefinition("prepareRequest", raw); err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func ValidateListResponse(response ListResponse) error {
	return validateQueryResponse("listResponse", response, response.Protocol, response.CollectionID, response.ObservedAt.IsZero(), response.Items)
}

func ValidateGetResponse(response GetResponse) error {
	return validateQueryResponse("getResponse", response, response.Protocol, response.CollectionID, response.ObservedAt.IsZero(), []ResourceView{response.Item})
}

func validateQueryResponse(definition string, response any, protocol, collectionID string, zeroTime bool, items []ResourceView) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if err := validateDefinition(definition, raw); err != nil {
		return err
	}
	if protocol != Protocol || collectionID == "" || zeroTime {
		return errors.New("Configuration Resource query 响应身份无效")
	}
	seen := map[string]struct{}{}
	for _, item := range items {
		if _, duplicate := seen[item.ResourceID]; duplicate {
			return errors.New("Configuration Resource query 返回重复资源")
		}
		seen[item.ResourceID] = struct{}{}
		if len(item.Values) == 0 || len(item.Values) > maxValuesBytes || !json.Valid(item.Values) || item.UpdatedAt.IsZero() {
			return errors.New("Configuration Resource query 返回无效资源")
		}
		fields := map[string]struct{}{}
		for _, state := range item.CredentialStates {
			if _, duplicate := fields[state.FieldID]; duplicate || state.Configured != (state.Version > 0) {
				return errors.New("Configuration Resource query 返回无效凭证状态")
			}
			fields[state.FieldID] = struct{}{}
		}
	}
	return nil
}

func ValidateObservation(observation Observation) error {
	raw, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	if err := validateDefinition("observation", raw); err != nil {
		return err
	}
	if observation.Protocol != Protocol || observation.ObservedAt.IsZero() {
		return errors.New("Configuration Resource observation 身份无效")
	}
	if observation.Candidate == nil {
		return nil
	}
	candidate := observation.Candidate
	switch candidate.Status {
	case StatusPrepared:
		if !candidate.Ready {
			return errors.New("Prepared 配置资源候选尚未 Ready")
		}
	case StatusCommitted:
		if !candidate.Ready || (candidate.Action == ActionDelete) != (observation.Active == nil) {
			return errors.New("Committed 配置资源候选未成为目标 Active 状态")
		}
		if observation.Active != nil && observation.Active.Digest != candidate.ResultDigest {
			return errors.New("Committed 配置资源摘要不一致")
		}
	case StatusAborted:
		if candidate.Ready {
			return errors.New("Aborted 配置资源候选不得 Ready")
		}
	default:
		return errors.New("Configuration Resource candidate 状态无效")
	}
	return nil
}
