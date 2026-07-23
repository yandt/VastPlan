package deploymentmanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationactivation"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var errConfigurationActivation = errors.New("应用插件配置激活失败")

// CreateConfigurationActivation creates an already-submitted service revision.
// The exact active catalog is revalidated inside deployment-manager; neither
// plugin-settings nor the browser can choose the target unit, plugin or schema.
func (s *Service) CreateConfigurationActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request configurationactivation.CreateRequest) (configurationactivation.Activation, error) {
	if err := request.Validate(); err != nil {
		return configurationactivation.Activation{}, errInvalid
	}
	tenant, err := callTenant(call)
	if err != nil {
		return configurationactivation.Activation{}, err
	}
	principal, err := actor(call)
	if err != nil {
		return configurationactivation.Activation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	requestHash := configurationActivationRequestHash(request)
	if existing, ok := configurationRevisionByCandidate(state, request.CandidateID); ok {
		if existing.ConfigurationID != request.ConfigurationID || state.ConfigurationRequests[request.CandidateID] != requestHash || existing.ConfigurationCatalog.Digest == "" {
			return configurationactivation.Activation{}, errServiceState
		}
		return activationFromRevision(existing), nil
	}
	active, definition, err := activeConfigurationDefinition(state, request)
	if err != nil {
		return configurationactivation.Activation{}, err
	}
	composition, err := patchApplicationConfiguration(active, definition, request)
	if err != nil {
		return configurationactivation.Activation{}, err
	}
	id := state.NextRevision + 1
	composition, err = normalizeServiceComposition(composition, tenant, id)
	if err != nil {
		return configurationactivation.Activation{}, errInvalid
	}
	preview, err := previewService(ctx, host, call, composition, id)
	if err != nil || !previewMatchesConfigurationRequest(preview.ConfigurationCatalog, request) {
		return configurationactivation.Activation{}, errConfigurationActivation
	}
	now := s.now().Format(time.RFC3339Nano)
	revision := platformadminapi.ServiceRevision{
		ID: id, Deployment: composition.Metadata.Name, Status: platformadminapi.ServicePendingApproval,
		Composition: composition, Preview: preview.Deployment, PreviewDigest: preview.Digest,
		ArtifactReferences: preview.ArtifactReferences, ConfigurationCatalog: preview.ConfigurationCatalog,
		ConfigurationCandidateID: request.CandidateID, ConfigurationID: request.ConfigurationID,
		PreviousServiceRevision: active.ID, SubmittedBy: principal, CreatedAt: now, UpdatedAt: now,
	}
	state.NextRevision = id
	state.Revisions = append(state.Revisions, revision)
	state.ConfigurationRequests[request.CandidateID] = requestHash
	s.auditServiceLocked(state, revision, "service.configuration.submitted", principal)
	if err := s.saveLocked(); err != nil {
		state.Revisions = state.Revisions[:len(state.Revisions)-1]
		state.NextRevision--
		delete(state.ConfigurationRequests, request.CandidateID)
		return configurationactivation.Activation{}, err
	}
	return activationFromRevision(revision), nil
}

func configurationActivationRequestHash(request configurationactivation.CreateRequest) string {
	raw, _ := json.Marshal(request)
	digest := sha256.Sum256(raw)
	return fmt.Sprintf("%x", digest[:])
}

func (s *Service) GetConfigurationActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request configurationactivation.LookupRequest) (configurationactivation.Activation, error) {
	if err := request.Validate(); err != nil {
		return configurationactivation.Activation{}, errInvalid
	}
	tenant, err := callTenant(call)
	if err != nil {
		return configurationactivation.Activation{}, err
	}
	s.mu.Lock()
	revision, ok := configurationRevisionByCandidate(s.tenantLocked(tenant), request.CandidateID)
	s.mu.Unlock()
	if !ok {
		return configurationactivation.Activation{}, errNotFound
	}
	activation := activationFromRevision(revision)
	if revision.RollbackServiceRevision != 0 {
		activation.Status = configurationactivation.StatusRolledBack
		return activation, nil
	}
	if revision.Status == platformadminapi.ServicePublished && !revision.Active {
		activation.Status = configurationactivation.StatusFailed
		activation.ErrorCode = "platform.plugin_configuration.revision_superseded"
		activation.ErrorMessage = "配置修订已被其他发布取代"
		return activation, nil
	}
	if revision.Status != platformadminapi.ServicePublished || !revision.Active {
		return activation, nil
	}
	observation, err := observeServiceReadiness(ctx, host, call, revision.Deployment, revision.ID)
	if err != nil {
		activation.Status, activation.ErrorCode, activation.ErrorMessage = configurationactivation.StatusPublishing, "platform.plugin_configuration.readiness_unknown", err.Error()
		return activation, nil
	}
	switch observation.Status {
	case deploymentpublication.ReadinessReady:
		activation.Status = configurationactivation.StatusReady
	case deploymentpublication.ReadinessFailed, deploymentpublication.ReadinessStopped:
		activation.Status, activation.ErrorCode, activation.ErrorMessage = configurationactivation.StatusFailed, "platform.plugin_configuration.candidate_not_ready", observation.Reason
	default:
		activation.Status = configurationactivation.StatusPublishing
	}
	return activation, nil
}

// PublishConfigurationActivation is idempotent for a candidate-bound revision.
// It waits for exact revision readiness and publishes a monotonic rollback when
// the candidate fails, so plugin-settings never needs direct KV or Node access.
func (s *Service) PublishConfigurationActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request configurationactivation.LookupRequest) (configurationactivation.Activation, error) {
	activation, err := s.GetConfigurationActivation(ctx, host, call, request)
	if err != nil {
		return configurationactivation.Activation{}, err
	}
	if activation.Status == configurationactivation.StatusReady || activation.Status == configurationactivation.StatusRolledBack {
		return activation, nil
	}
	tenant, _ := callTenant(call)
	s.mu.Lock()
	revision, ok := configurationRevisionByCandidate(s.tenantLocked(tenant), request.CandidateID)
	s.mu.Unlock()
	if !ok || (revision.Status != platformadminapi.ServiceApproved && revision.Status != platformadminapi.ServicePublishing && revision.Status != platformadminapi.ServicePublished) {
		return configurationactivation.Activation{}, errServiceState
	}
	if revision.Status != platformadminapi.ServicePublished {
		if _, err := s.publishServiceRevision(ctx, host, call, revision.ID, true); err != nil {
			return configurationactivation.Activation{}, err
		}
	}
	waitCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, s.releaseTimeout)
		defer cancel()
	}
	if err := s.waitForServiceReadiness(waitCtx, host, call, revision.Deployment, revision.ID); err == nil {
		return s.GetConfigurationActivation(ctx, host, call, request)
	} else {
		return s.rollbackConfigurationActivation(ctx, host, call, request, revision, err)
	}
}

func (s *Service) rollbackConfigurationActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request configurationactivation.LookupRequest, failed platformadminapi.ServiceRevision, cause error) (configurationactivation.Activation, error) {
	if failed.PreviousServiceRevision == 0 {
		return configurationactivation.Activation{}, fmt.Errorf("%w: %v", errConfigurationActivation, cause)
	}
	rollback, err := s.RollbackServiceRevision(ctx, host, call, failed.PreviousServiceRevision)
	if err == nil {
		err = s.waitForServiceReadiness(ctx, host, call, rollback.Deployment, rollback.ID)
	}
	if err != nil {
		return configurationactivation.Activation{}, fmt.Errorf("%w: rollback: %v", errConfigurationActivation, err)
	}
	tenant, _ := callTenant(call)
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	index, lookupErr := serviceRevisionIndex(state, failed.ID)
	if lookupErr == nil {
		state.Revisions[index].RollbackServiceRevision = rollback.ID
		state.Revisions[index].UpdatedAt = s.now().Format(time.RFC3339Nano)
		s.auditServiceLocked(state, state.Revisions[index], "service.configuration.rolled_back", actorOrUnknown(call))
		lookupErr = s.saveLocked()
	}
	s.mu.Unlock()
	if lookupErr != nil {
		return configurationactivation.Activation{}, lookupErr
	}
	activation, getErr := s.GetConfigurationActivation(ctx, host, call, request)
	activation.ErrorCode, activation.ErrorMessage = "platform.plugin_configuration.candidate_not_ready", cause.Error()
	return activation, getErr
}

func activeConfigurationDefinition(state *tenantState, request configurationactivation.CreateRequest) (platformadminapi.ServiceRevision, pluginconfiguration.Definition, error) {
	var matchedRevision platformadminapi.ServiceRevision
	var matchedDefinition pluginconfiguration.Definition
	for _, revision := range state.Revisions {
		if !revision.Active || revision.Status != platformadminapi.ServicePublished || revision.ConfigurationCatalog.Digest != request.CatalogDigest {
			continue
		}
		for _, definition := range revision.ConfigurationCatalog.Items {
			if definition.ID != request.ConfigurationID {
				continue
			}
			if matchedRevision.ID != 0 || definition.ApplyPath != pluginconfiguration.ApplyApplicationDeployment ||
				definition.SchemaDigest != request.SchemaDigest || definition.Artifact.SHA256 != request.ArtifactSHA256 {
				return platformadminapi.ServiceRevision{}, pluginconfiguration.Definition{}, errServiceState
			}
			matchedRevision, matchedDefinition = cloneServiceRevision(revision), definition
		}
	}
	if matchedRevision.ID == 0 {
		return platformadminapi.ServiceRevision{}, pluginconfiguration.Definition{}, errNotFound
	}
	if err := pluginconfiguration.ValidateValues(matchedDefinition, request.Values); err != nil {
		return platformadminapi.ServiceRevision{}, pluginconfiguration.Definition{}, errInvalid
	}
	return matchedRevision, matchedDefinition, nil
}

func patchApplicationConfiguration(active platformadminapi.ServiceRevision, definition pluginconfiguration.Definition, request configurationactivation.CreateRequest) (backendcompositionv1.ApplicationComposition, error) {
	composition := cloneJSON(active.Composition)
	for index := range composition.Units {
		unit := &composition.Units[index].Spec
		if unit.ID != definition.UnitID {
			continue
		}
		installed := make([]string, 0, len(unit.Plugins))
		foundPlugin := false
		for _, plugin := range unit.Plugins {
			installed = append(installed, plugin.ID)
			if plugin.ID == definition.PluginID {
				foundPlugin = true
			}
		}
		if !foundPlugin {
			return backendcompositionv1.ApplicationComposition{}, errServiceState
		}
		envelope, parseErr := pluginconfig.Parse(unit.Config, installed)
		if parseErr != nil {
			return backendcompositionv1.ApplicationComposition{}, parseErr
		}
		var values map[string]any
		if json.Unmarshal(request.Values, &values) != nil || values == nil {
			return backendcompositionv1.ApplicationComposition{}, errInvalid
		}
		envelope.Plugins[definition.PluginID] = values
		if envelope.ManagedCredentials[definition.PluginID] == nil {
			envelope.ManagedCredentials[definition.PluginID] = map[string]pluginconfig.ManagedCredentialRef{}
		}
		for fieldID, ref := range request.Credentials {
			envelope.ManagedCredentials[definition.PluginID][fieldID] = ref
		}
		if err := validateFinalCredentialRefs(definition, envelope.ManagedCredentials[definition.PluginID]); err != nil {
			return backendcompositionv1.ApplicationComposition{}, err
		}
		unit.Config = envelope.Map()
		return composition, nil
	}
	return backendcompositionv1.ApplicationComposition{}, errNotFound
}

func validateFinalCredentialRefs(definition pluginconfiguration.Definition, refs map[string]pluginconfig.ManagedCredentialRef) error {
	declared := make(map[string]pluginv1.ManagedCredentialField, len(definition.ManagedCredentials))
	for _, field := range definition.ManagedCredentials {
		declared[field.ID] = field
		ref, ok := refs[field.ID]
		if field.Required && !ok {
			return errInvalid
		}
		if ok && (ref.Owner != definition.PluginID || ref.Purpose != field.Purpose) {
			return errInvalid
		}
	}
	for fieldID := range refs {
		if _, ok := declared[fieldID]; !ok {
			return errInvalid
		}
	}
	return nil
}

func previewMatchesConfigurationRequest(catalog pluginconfiguration.Catalog, request configurationactivation.CreateRequest) bool {
	if catalog.Validate() != nil {
		return false
	}
	for _, definition := range catalog.Items {
		if definition.ID == request.ConfigurationID {
			return definition.SchemaDigest == request.SchemaDigest && definition.Artifact.SHA256 == request.ArtifactSHA256 && jsonEqual(definition.Values, request.Values)
		}
	}
	return false
}

func jsonEqual(left, right []byte) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && bytes.Equal(mustJSON(a), mustJSON(b))
}

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

func configurationRevisionByCandidate(state *tenantState, candidateID string) (platformadminapi.ServiceRevision, bool) {
	for _, revision := range state.Revisions {
		if revision.ConfigurationCandidateID == candidateID {
			return cloneServiceRevision(revision), true
		}
	}
	return platformadminapi.ServiceRevision{}, false
}

func activationFromRevision(revision platformadminapi.ServiceRevision) configurationactivation.Activation {
	status := configurationactivation.StatusPendingApproval
	switch revision.Status {
	case platformadminapi.ServiceApproved:
		status = configurationactivation.StatusApproved
	case platformadminapi.ServicePublishing, platformadminapi.ServicePublished:
		status = configurationactivation.StatusPublishing
	}
	if revision.RollbackServiceRevision != 0 {
		status = configurationactivation.StatusRolledBack
	}
	return configurationactivation.Activation{
		CandidateID: revision.ConfigurationCandidateID, ConfigurationID: revision.ConfigurationID, Deployment: revision.Deployment,
		ServiceRevision: revision.ID, PreviousServiceRevision: revision.PreviousServiceRevision,
		RollbackServiceRevision: revision.RollbackServiceRevision, Status: status,
	}
}

func observeServiceReadiness(ctx context.Context, host sdk.Host, call *contractv1.CallContext, deployment string, revision uint64) (deploymentpublication.ReadinessObservation, error) {
	var observation deploymentpublication.ReadinessObservation
	err := callKernelDeployment(ctx, host, call, deploymentpublication.KernelReadinessService, deploymentpublication.ReadinessRequest{DeploymentName: deployment, DeploymentRevision: revision}, &observation)
	if err != nil {
		return observation, err
	}
	if err := observation.Validate(); err != nil || observation.Deployment != deployment || observation.Revision != revision {
		return observation, errors.New("部署 readiness observation 身份不匹配")
	}
	return observation, nil
}
