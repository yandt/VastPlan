package pluginsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type resourceDraftRequest struct {
	ConfigurationID      string
	ResourceCollectionID string
	ResourceID           string
	CatalogDigest        string
	Action               configurationresourcev1.Action
	Values               json.RawMessage
	Secrets              map[string]string
}

func (s *Service) ListResourceItems(ctx context.Context, host sdk.Host, call *contractv1.CallContext, configurationID, collectionID, catalogDigest, cursor string, limit uint32) (configurationresourcev1.ListResponse, error) {
	definition, collection, err := s.resourceDefinition(ctx, host, call, configurationID, collectionID, catalogDigest)
	if err != nil {
		return configurationresourcev1.ListResponse{}, err
	}
	if limit == 0 || limit > collection.MaxItems {
		limit = collection.MaxItems
	}
	return listResourceItems(ctx, host, call, *definition.ResourceController, configurationresourcev1.ListRequest{CollectionID: collection.ID, Cursor: cursor, Limit: limit})
}

func (s *Service) GetResourceItem(ctx context.Context, host sdk.Host, call *contractv1.CallContext, configurationID, collectionID, resourceID, catalogDigest string) (configurationresourcev1.GetResponse, error) {
	definition, collection, err := s.resourceDefinition(ctx, host, call, configurationID, collectionID, catalogDigest)
	if err != nil {
		return configurationresourcev1.GetResponse{}, err
	}
	return getResourceItem(ctx, host, call, *definition.ResourceController, configurationresourcev1.GetRequest{CollectionID: collection.ID, ResourceID: resourceID})
}

func (s *Service) CreateResourceDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request resourceDraftRequest) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || request.ConfigurationID == "" || request.ResourceCollectionID == "" || len(request.CatalogDigest) != 64 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	definition, collection, err := s.resourceDefinition(ctx, host, call, request.ConfigurationID, request.ResourceCollectionID, request.CatalogDigest)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	resourceID, active, states, err := s.resourceDraftBaseline(ctx, host, call, definition, collection, request)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if request.Action == configurationresourcev1.ActionDelete {
		if len(request.Values) > 0 || len(request.Secrets) > 0 {
			return pluginconfiguration.Candidate{}, ErrInvalid
		}
	} else if err := pluginconfiguration.ValidateResourceValues(collection, request.Values); err != nil {
		return pluginconfiguration.Candidate{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	credentialStatus, err := credentialStatusesFor(collection.ManagedCredentials, states, request.Secrets)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if request.Action == configurationresourcev1.ActionDelete {
		credentialStatus = nil
	}
	candidateID, err := s.newID()
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	now := s.now().Format(time.RFC3339Nano)
	status := pluginconfiguration.CandidateDraft
	if len(request.Secrets) > 0 {
		status = pluginconfiguration.CandidatePreparing
	}
	candidate := pluginconfiguration.Candidate{
		ID: candidateID, ConfigurationID: definition.ID, ResourceCollectionID: collection.ID, ResourceID: resourceID,
		ResourceAction: string(request.Action), Revision: 1, Status: status, ApplyPath: pluginconfiguration.ApplyResourceProfile,
		CatalogDigest: request.CatalogDigest, SchemaDigest: collection.SchemaDigest, ArtifactSHA256: definition.Artifact.SHA256,
		Values: append(json.RawMessage(nil), request.Values...), CreatedBy: actor, CreatedAt: now, UpdatedAt: now,
		ManagedCredentials: credentialStatus,
	}
	if active != nil {
		candidate.ExternalRevision, candidate.ExternalDigest = active.Revision, active.Digest
	}
	if err := s.saveResourceDraft(tenant, candidate); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if len(request.Secrets) == 0 {
		return cloneCandidate(candidate), nil
	}
	stagingTarget := credentialStagingTarget{
		ConfigurationID: definition.ID, ResourceCollectionID: collection.ID, ResourceID: resourceID,
		PluginID: definition.PluginID, Fields: collection.ManagedCredentials,
	}
	staged, stageErr := s.stageSecretsFor(ctx, host, call, stagingTarget, candidate.ID, request.CatalogDigest, request.Secrets, func(fieldID string, stage pluginconfig.StagedCredential) error {
		return s.checkpointCredential(tenant, candidate.ID, fieldID, stage)
	})
	if stageErr != nil {
		abortErr := abortCredentialStages(ctx, host, call, candidate.ID, staged)
		return pluginconfiguration.Candidate{}, errors.Join(stageErr, s.failPreparing(tenant, candidate.ID, actor, abortErr))
	}
	return s.finishPreparing(tenant, candidate.ID, actor)
}

func (s *Service) resourceDefinition(ctx context.Context, host sdk.Host, call *contractv1.CallContext, configurationID, collectionID, catalogDigest string) (pluginconfiguration.Definition, pluginconfiguration.ResourceCollection, error) {
	catalogs, err := s.catalogs(ctx, host, call)
	if err != nil {
		return pluginconfiguration.Definition{}, pluginconfiguration.ResourceCollection{}, err
	}
	view, err := findDefinition(catalogs, configurationID, catalogDigest)
	if err != nil {
		return pluginconfiguration.Definition{}, pluginconfiguration.ResourceCollection{}, err
	}
	if view.ResourceController == nil || view.ResourceController.Protocol != configurationresourcev1.Protocol {
		return pluginconfiguration.Definition{}, pluginconfiguration.ResourceCollection{}, fmt.Errorf("%w: 目标插件未实现 configuration.resource.v1", ErrInvalid)
	}
	for _, collection := range view.ResourceCollections {
		if collection.ID == collectionID {
			return view.Definition, collection, nil
		}
	}
	return pluginconfiguration.Definition{}, pluginconfiguration.ResourceCollection{}, ErrNotFound
}

func (s *Service) resourceDraftBaseline(ctx context.Context, host sdk.Host, call *contractv1.CallContext, definition pluginconfiguration.Definition, collection pluginconfiguration.ResourceCollection, request resourceDraftRequest) (string, *configurationresourcev1.ActiveReference, []pluginconfiguration.CredentialState, error) {
	switch request.Action {
	case configurationresourcev1.ActionCreate:
		if request.ResourceID != "" {
			return "", nil, nil, ErrInvalid
		}
		page, err := listResourceItems(ctx, host, call, *definition.ResourceController, configurationresourcev1.ListRequest{CollectionID: collection.ID, Limit: collection.MaxItems})
		if err != nil {
			return "", nil, nil, err
		}
		if page.NextCursor != "" || uint32(len(page.Items)) >= collection.MaxItems {
			return "", nil, nil, ErrConflict
		}
		resourceID, err := s.newResourceID()
		return resourceID, nil, nil, err
	case configurationresourcev1.ActionUpdate, configurationresourcev1.ActionDelete:
		if request.ResourceID == "" {
			return "", nil, nil, ErrInvalid
		}
		item, err := getResourceItem(ctx, host, call, *definition.ResourceController, configurationresourcev1.GetRequest{CollectionID: collection.ID, ResourceID: request.ResourceID})
		if err != nil {
			return "", nil, nil, err
		}
		if request.Action == configurationresourcev1.ActionDelete && collection.MinItems > 0 {
			page, listErr := listResourceItems(ctx, host, call, *definition.ResourceController, configurationresourcev1.ListRequest{CollectionID: collection.ID, Limit: collection.MaxItems})
			if listErr != nil {
				return "", nil, nil, listErr
			}
			if page.NextCursor != "" || uint32(len(page.Items)) <= collection.MinItems {
				return "", nil, nil, ErrConflict
			}
		}
		active := item.Item.Active
		states := make([]pluginconfiguration.CredentialState, 0, len(item.Item.CredentialStates))
		for _, state := range item.Item.CredentialStates {
			states = append(states, pluginconfiguration.CredentialState{FieldID: state.FieldID, Configured: state.Configured, Version: state.Version})
		}
		return request.ResourceID, &active, states, nil
	default:
		return "", nil, nil, ErrInvalid
	}
}

func (s *Service) saveResourceDraft(tenant string, candidate pluginconfiguration.Candidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	if len(state.Candidates) >= maxCandidates || state.Current[candidate.ResourceID] != "" {
		return ErrConflict
	}
	state.Candidates[candidate.ID], state.Current[candidate.ResourceID] = candidate, candidate.ID
	state.CredentialStages[candidate.ID] = map[string]credentialStage{}
	s.auditLocked(state, candidate, "configuration.resource.draft.created", candidate.CreatedBy)
	if err := s.saveLocked(); err != nil {
		delete(state.Candidates, candidate.ID)
		delete(state.Current, candidate.ResourceID)
		delete(state.CredentialStages, candidate.ID)
		state.Audit, state.NextAudit = state.Audit[:len(state.Audit)-1], state.NextAudit-1
		return err
	}
	return nil
}
