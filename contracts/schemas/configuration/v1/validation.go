package configurationv1

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

//go:embed vastplan.configuration-controller.schema.json
var schemaJSON []byte

var (
	schemaOnce sync.Once
	schemas    map[string]*jsonschema.Schema
	schemaErr  error
)

var requestDefinitions = map[string]string{
	OperationPrepare: "prepareRequest",
	OperationCommit:  "candidateRequest",
	OperationAbort:   "candidateRequest",
	OperationStatus:  "statusRequest",
}

func compileSchemas() {
	compiler := jsonschema.NewCompiler()
	if err := commonv1.AddResources(compiler); err != nil {
		schemaErr = err
		return
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		schemaErr = fmt.Errorf("解析 Configuration Controller Schema: %w", err)
		return
	}
	if err := compiler.AddResource(SchemaURL, document); err != nil {
		schemaErr = fmt.Errorf("登记 Configuration Controller Schema: %w", err)
		return
	}
	schemas = map[string]*jsonschema.Schema{}
	for _, definition := range []string{"prepareRequest", "candidateRequest", "statusRequest", "observation"} {
		compiled, compileErr := compiler.Compile(SchemaURL + "#/$defs/" + definition)
		if compileErr != nil {
			schemaErr = fmt.Errorf("编译 Configuration Controller Schema %s: %w", definition, compileErr)
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
		return fmt.Errorf("解析 Configuration Controller JSON: %w", err)
	}
	if err := schemas[definition].Validate(instance); err != nil {
		return fmt.Errorf("Configuration Controller %s 不符合 Schema: %w", definition, err)
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
		return errors.New("Configuration Controller 只能包含一个 JSON 文档")
	}
	return nil
}

func ParseRequest(operation string, raw []byte) (any, error) {
	definition, ok := requestDefinitions[operation]
	if !ok {
		return nil, fmt.Errorf("不支持的 Configuration Controller 操作 %q", operation)
	}
	if err := validateDefinition(definition, raw); err != nil {
		return nil, err
	}
	var target any
	switch operation {
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
		if _, err := NormalizePrepareRequest(*request); err != nil {
			return nil, err
		}
	case *StatusRequest:
		if (request.CandidateID == "") != (request.RequestDigest == "") {
			return nil, errors.New("status 的 candidateId 与 requestDigest 必须同时提供")
		}
	}
	return target, nil
}

func NormalizePrepareRequest(request PrepareRequest) (PrepareRequest, error) {
	if len(request.Values) == 0 || len(request.Values) > maxValuesBytes {
		return PrepareRequest{}, errors.New("Configuration Controller values 大小无效")
	}
	var values map[string]any
	if err := json.Unmarshal(request.Values, &values); err != nil || values == nil {
		return PrepareRequest{}, errors.New("Configuration Controller values 必须是 JSON 对象")
	}
	request.Values, _ = json.Marshal(values)
	credentials := make(map[string]commonv1.ManagedCredentialRef, len(request.ManagedCredentials))
	for fieldID, ref := range request.ManagedCredentials {
		if strings.TrimSpace(fieldID) == "" || !strings.HasPrefix(ref.Handle, "credential://managed/") || ref.Scope != "tenant" ||
			strings.TrimSpace(ref.Owner) == "" || strings.TrimSpace(ref.Purpose) == "" || ref.Version < 1 {
			return PrepareRequest{}, errors.New("Configuration Controller 包含无效托管凭证引用")
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

func DigestConfiguration(values json.RawMessage, credentials map[string]commonv1.ManagedCredentialRef) (string, error) {
	normalized, err := NormalizePrepareRequest(PrepareRequest{Values: values, ManagedCredentials: credentials})
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Values             json.RawMessage                          `json:"values"`
		ManagedCredentials map[string]commonv1.ManagedCredentialRef `json:"managedCredentials,omitempty"`
	}{Values: normalized.Values, ManagedCredentials: normalized.ManagedCredentials})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
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
		return errors.New("Configuration Controller observation 身份无效")
	}
	if observation.Candidate != nil {
		candidate := observation.Candidate
		switch candidate.Status {
		case StatusPrepared:
		case StatusCommitted:
			if !candidate.Ready || observation.Active.Digest != candidate.ConfigurationDigest {
				return errors.New("Committed 配置候选未成为 Active")
			}
		case StatusAborted:
			if candidate.Ready {
				return errors.New("Aborted 配置候选不得 Ready")
			}
		default:
			return errors.New("Configuration Controller candidate 状态无效")
		}
	}
	return nil
}
