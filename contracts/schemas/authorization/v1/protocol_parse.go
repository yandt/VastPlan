package authorizationv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type messageFactory func() any

type operationContract struct {
	schemaURL      string
	requestDef     string
	requestFactory messageFactory
	resultDef      string
	resultFactory  messageFactory
}

var providerContracts = map[string]map[string]operationContract{
	ProtocolStore: {
		"probe":          contract(storeSchemaURL, "probe", func() any { return &StoreProbeRequest{} }, func() any { return &StoreProbeResult{} }),
		"load":           contract(storeSchemaURL, "load", func() any { return &StoreLoadRequest{} }, func() any { return &StoreLoadResult{} }),
		"compareAndSwap": contract(storeSchemaURL, "compareAndSwap", func() any { return &StoreCompareAndSwapRequest{} }, func() any { return &StoreCompareAndSwapResult{} }),
		"watch":          contract(storeSchemaURL, "watch", func() any { return &StoreWatchRequest{} }, func() any { return &StoreWatchResult{} }),
		"appendAudit":    contract(storeSchemaURL, "appendAudit", func() any { return &StoreAppendAuditRequest{} }, func() any { return &StoreAppendAuditResult{} }),
		"backup":         contract(storeSchemaURL, "backup", func() any { return &StoreBackupRequest{} }, func() any { return &StoreBackupResult{} }),
	},
	ProtocolEngine: {
		"prepare":  contract(engineSchemaURL, "prepare", func() any { return &EnginePrepareRequest{} }, func() any { return &EnginePrepareResult{} }),
		"evaluate": contract(engineSchemaURL, "evaluate", func() any { return &EngineEvaluateRequest{} }, func() any { return &EngineEvaluateResult{} }),
		"explain":  contract(engineSchemaURL, "explain", func() any { return &EngineExplainRequest{} }, func() any { return &EngineExplainResult{} }),
		"health":   contract(engineSchemaURL, "health", func() any { return &EngineHealthRequest{} }, func() any { return &EngineHealthResult{} }),
	},
	ProtocolDirectory: {
		"resolveSubject": contract(directorySchemaURL, "resolveSubject", func() any { return &DirectoryResolveSubjectRequest{} }, func() any { return &DirectoryResolveSubjectResult{} }),
		"resolveGroups":  contract(directorySchemaURL, "resolveGroups", func() any { return &DirectoryResolveGroupsRequest{} }, func() any { return &DirectoryResolveGroupsResult{} }),
		"watchRevision":  contract(directorySchemaURL, "watchRevision", func() any { return &DirectoryWatchRevisionRequest{} }, func() any { return &DirectoryWatchRevisionResult{} }),
	},
	ProtocolExchange: {
		"planImport": contract(exchangeSchemaURL, "planImport", func() any { return &ExchangePlanImportRequest{} }, func() any { return &ExchangePlanImportResult{} }),
		"validate":   contract(exchangeSchemaURL, "validate", func() any { return &ExchangeValidateRequest{} }, func() any { return &ExchangeValidateResult{} }),
		"import":     contract(exchangeSchemaURL, "import", func() any { return &ExchangeImportRequest{} }, func() any { return &ExchangeImportResult{} }),
		"export":     contract(exchangeSchemaURL, "export", func() any { return &ExchangeExportRequest{} }, func() any { return &ExchangeExportResult{} }),
	},
}

func contract(schemaURL, name string, request, result messageFactory) operationContract {
	return operationContract{schemaURL: schemaURL, requestDef: name + "Request", requestFactory: request, resultDef: name + "Result", resultFactory: result}
}

func ParseProviderRequest(protocol, operation string, raw []byte) (any, error) {
	return parseProviderMessage(protocol, operation, raw, true)
}

func ParseProviderResult(protocol, operation string, raw []byte) (any, error) {
	return parseProviderMessage(protocol, operation, raw, false)
}

func parseProviderMessage(protocol, operation string, raw []byte, request bool) (any, error) {
	operations := providerContracts[protocol]
	definition, exists := operations[operation]
	if !exists {
		return nil, fmt.Errorf("不支持的 Authorization Provider 操作 %q/%q", protocol, operation)
	}
	if limit := providerMessageLimit(protocol, operation); len(raw) > limit {
		return nil, fmt.Errorf("Authorization Provider 消息超过 %d bytes", limit)
	}
	name, factory := definition.resultDef, definition.resultFactory
	if request {
		name, factory = definition.requestDef, definition.requestFactory
	}
	if err := validateSchema(definition.schemaURL+"#/$defs/"+name, raw); err != nil {
		return nil, err
	}
	target := factory()
	if err := decodeStrict(raw, target); err != nil {
		return nil, err
	}
	if err := validateProviderSemantics(protocol, operation, target, request); err != nil {
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
		return errors.New("Authorization 消息只能包含一个 JSON 文档")
	}
	return nil
}
