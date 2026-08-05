package main

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *service) handle(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte, operation string) (*contractv1.CallResult, []byte, error) {
	tenantID, err := tenant(call)
	if err != nil {
		return nil, nil, err
	}
	if isPlatformControlOperation(operation) {
		return callPlatformControl(ctx, host, call, operation, payload)
	}
	// Configuration writes are infrequent. Serializing the complete local saga
	// prevents two concurrent updates of the same connection from activating an
	// older credential after a newer candidate has already won.
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := s.beginStateOperation(ctx, host, call, tenantID); err != nil {
		return nil, nil, err
	}
	defer s.endStateOperation()
	if err := s.reconcilePending(ctx, host, call, tenantID); err != nil {
		return nil, nil, err
	}
	if err := s.reconcileTestCredentials(ctx, host, call, tenantID); err != nil {
		return nil, nil, err
	}
	if operation == "resolveRuntime" {
		return s.resolveRuntime(call, payload, tenantID)
	}
	// Persistent outboxes advance opportunistically; an unavailable Runtime
	// does not hide saved management definitions or block reads.
	_ = s.reconcilePublications(ctx, host, call, tenantID)
	_ = s.reconcileRetire(ctx, host, call, tenantID)
	return s.handleManagement(ctx, host, call, payload, operation, tenantID)
}

func (s *service) resolveRuntime(call *contractv1.CallContext, payload []byte, tenantID string) (*contractv1.CallResult, []byte, error) {
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() != databasev1.RuntimePluginID {
		return domainError("platform.database.forbidden", errors.New("只有 Database Runtime 可以解析内部连接定义"))
	}
	var input struct {
		Connection databasev1.ConnectionRef `json:"connection"`
	}
	if err := json.Unmarshal(payload, &input); err != nil || databasev1.ValidateConnectionRef(input.Connection) != nil {
		return domainError("platform.database.invalid_request", errors.New("连接引用无效"))
	}
	s.mu.RLock()
	var found *definition
	for _, candidate := range s.data.Tenants[tenantID] {
		if candidate.ResourceID == input.Connection.ResourceID && candidate.Revision == input.Connection.Revision {
			copy := candidate
			found = &copy
			break
		}
	}
	s.mu.RUnlock()
	if found == nil {
		return domainError("platform.database.not_found", errConnectionNotFound)
	}
	raw, err := json.Marshal(databasev1.ActivateRequest{Connection: connectionSpec(*found)})
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (s *service) handleManagement(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte, operation, tenantID string) (*contractv1.CallResult, []byte, error) {
	var output any
	if operation == "define" {
		var input defineInput
		if err := json.Unmarshal(payload, &input); err != nil {
			return nil, nil, err
		}
		saved, err := s.define(ctx, host, call, tenantID, input)
		if err != nil {
			if issue, ok := databasev1.ValidationIssueFrom(err); ok {
				return domainErrorDetails("platform.database.invalid", err, validationDetails(issue.Field, issue.Reason))
			}
			return nil, nil, err
		}
		_ = s.reconcilePublications(ctx, host, call, tenantID)
		_ = s.reconcileRetire(ctx, host, call, tenantID)
		output = view(saved, s.runtimeStatus(tenantID, saved))
	} else if operation == "test" {
		var input defineInput
		if err := json.Unmarshal(payload, &input); err != nil {
			return nil, nil, err
		}
		var err error
		output, err = s.testConnection(ctx, host, call, tenantID, input)
		if err != nil {
			return databaseTestError(err)
		}
	} else {
		var input struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(payload, &input); err != nil {
			return nil, nil, err
		}
		var err error
		switch operation {
		case "describe":
			output, err = s.describe(tenantID, input.Name)
		case "list":
			output = s.list(tenantID)
		case "remove":
			output, err = s.remove(ctx, host, call, tenantID, input.Name)
		case "probe":
			output, err = s.probe(ctx, host, call, tenantID, input.Name)
		default:
			return nil, nil, errors.New("不支持的数据库操作")
		}
		if errors.Is(err, errConnectionNotFound) {
			return domainError("platform.database.not_found", err)
		}
		if err != nil {
			if operation == "probe" {
				return databaseTestError(err)
			}
			return nil, nil, err
		}
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func validationDetails(field, reason string) map[string]string {
	return map[string]string{"validationField": field, "validationReason": reason}
}

func (s *service) runtimeStatus(tenantID string, value definition) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if publication, pending := s.data.Publications[tenantID][value.Name]; pending && publication.Connection.Revision == value.Revision {
		return "pending"
	}
	return "ready"
}

func (s *service) describe(tenantID, name string) (definitionView, error) {
	s.mu.RLock()
	value, ok := s.data.Tenants[tenantID][name]
	s.mu.RUnlock()
	if !ok {
		return definitionView{}, errConnectionNotFound
	}
	return view(value, s.runtimeStatus(tenantID, value)), nil
}

func (s *service) list(tenantID string) []definitionView {
	s.mu.RLock()
	definitions := make([]definition, 0, len(s.data.Tenants[tenantID]))
	for _, value := range s.data.Tenants[tenantID] {
		definitions = append(definitions, value)
	}
	s.mu.RUnlock()
	items := make([]definitionView, 0, len(definitions))
	for _, value := range definitions {
		items = append(items, view(value, s.runtimeStatus(tenantID, value)))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func (s *service) remove(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenantID, name string) (map[string]any, error) {
	s.mu.Lock()
	value, ok := s.definitions(tenantID)[name]
	if !ok {
		s.mu.Unlock()
		return nil, errConnectionNotFound
	}
	delete(s.definitions(tenantID), name)
	previousPublication, publicationExists := s.publications(tenantID)[name]
	ref := value.CredentialRef
	s.publications(tenantID)[name] = runtimePublication{Action: databasev1.OperationRetire, Connection: value, RetireCredential: &ref}
	err := s.save()
	if err != nil {
		s.definitions(tenantID)[name] = value
		if publicationExists {
			s.publications(tenantID)[name] = previousPublication
		} else {
			delete(s.publications(tenantID), name)
		}
	}
	s.mu.Unlock()
	if err == nil {
		_ = s.reconcilePublications(ctx, host, call, tenantID)
		_ = s.reconcileRetire(ctx, host, call, tenantID)
	}
	return map[string]any{"name": name, "removed": err == nil}, err
}

func (s *service) probe(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenantID, name string) (databasev1.ProbeResult, error) {
	s.mu.RLock()
	value, ok := s.data.Tenants[tenantID][name]
	s.mu.RUnlock()
	if !ok {
		return databasev1.ProbeResult{}, errConnectionNotFound
	}
	var result databasev1.ProbeResult
	err := callRuntime(ctx, host, call, databasev1.OperationProbe, databasev1.ProbeRequest{Connection: connectionSpec(value)}, &result)
	return result, err
}
