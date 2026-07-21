package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

// PlatformCapabilityCaller is the only transport port used by the platform
// administration service. Capability and operation remain server-owned.
type PlatformCapabilityCaller interface {
	Call(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, string, []byte) ([]byte, error)
}

type CapabilityPlatformAdminService struct{ caller PlatformCapabilityCaller }

func NewCapabilityPlatformAdminService(caller PlatformCapabilityCaller) (*CapabilityPlatformAdminService, error) {
	if caller == nil {
		return nil, errors.New("平台管理 capability caller 不能为空")
	}
	return &CapabilityPlatformAdminService{caller: caller}, nil
}

func (s *CapabilityPlatformAdminService) call(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, capability, operation string, write bool, request, response any) error {
	if !target.Allows(capability, operation, write) {
		return portalapi.ErrForbidden
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	raw, err = s.caller.Call(ctx, p, target, capability, operation, raw)
	if err != nil {
		return err
	}
	if response == nil {
		return nil
	}
	if err := json.Unmarshal(raw, response); err != nil {
		return fmt.Errorf("平台能力 %s/%s 返回无效响应: %w", capability, operation, err)
	}
	return nil
}

func (s *CapabilityPlatformAdminService) ListSettings(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, prefix string) ([]platformadminapi.Setting, error) {
	var response struct {
		Items []platformadminapi.Setting `json:"items"`
	}
	err := s.call(ctx, p, target, platformadminapi.SettingsCapability, "list", false, map[string]string{"prefix": prefix}, &response)
	return response.Items, err
}

func (s *CapabilityPlatformAdminService) PutSetting(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, key string, request platformadminapi.PutSettingRequest) (platformadminapi.Setting, error) {
	if err := validResourceName(key, 320); err != nil || len(request.Value) == 0 || !json.Valid(request.Value) {
		return platformadminapi.Setting{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.Setting
	payload := struct {
		Key       string          `json:"key"`
		Value     json.RawMessage `json:"value"`
		IfVersion *int64          `json:"ifVersion,omitempty"`
	}{Key: key, Value: request.Value, IfVersion: request.IfVersion}
	if err := s.call(ctx, p, target, platformadminapi.SettingsCapability, "put", true, payload, &response); err != nil {
		return response, err
	}
	response.Value = append(json.RawMessage(nil), request.Value...)
	return response, nil
}

func (s *CapabilityPlatformAdminService) DeleteSetting(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, key string, ifVersion *int64) error {
	if err := validResourceName(key, 320); err != nil {
		return platformadminapi.ErrInvalid
	}
	return s.call(ctx, p, target, platformadminapi.SettingsCapability, "delete", true, struct {
		Key       string `json:"key"`
		IfVersion *int64 `json:"ifVersion,omitempty"`
	}{key, ifVersion}, nil)
}

func (s *CapabilityPlatformAdminService) ListCredentials(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, prefix string) ([]platformadminapi.CredentialMetadata, error) {
	var response []platformadminapi.CredentialMetadata
	err := s.call(ctx, p, target, platformadminapi.CredentialsCapability, "list", false, map[string]string{"prefix": prefix}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) PutCredential(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, name string, request platformadminapi.PutCredentialRequest) (platformadminapi.CredentialMetadata, error) {
	if err := validResourceName(name, 160); err != nil || request.Value == "" {
		return platformadminapi.CredentialMetadata{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.CredentialMetadata
	err := s.call(ctx, p, target, platformadminapi.CredentialsCapability, "put", true, map[string]string{"name": name, "value": request.Value}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) credentialAction(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, name, operation string) (platformadminapi.CredentialMetadata, error) {
	if err := validResourceName(name, 160); err != nil {
		return platformadminapi.CredentialMetadata{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.CredentialMetadata
	err := s.call(ctx, p, target, platformadminapi.CredentialsCapability, operation, true, map[string]string{"name": name}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) RotateCredential(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, name string) (platformadminapi.CredentialMetadata, error) {
	return s.credentialAction(ctx, p, target, name, "rotate")
}

func (s *CapabilityPlatformAdminService) RevokeCredential(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, name string) (platformadminapi.CredentialMetadata, error) {
	return s.credentialAction(ctx, p, target, name, "revoke")
}

func (s *CapabilityPlatformAdminService) ListDatabaseConnections(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) ([]platformadminapi.DatabaseConnection, error) {
	var response []platformadminapi.DatabaseConnection
	err := s.call(ctx, p, target, platformadminapi.DatabaseCapability, "list", false, struct{}{}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) PutDatabaseConnection(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, name string, request platformadminapi.PutDatabaseConnectionRequest) (platformadminapi.DatabaseConnection, error) {
	if err := validResourceName(name, 160); err != nil || strings.TrimSpace(request.ProviderID) == "" || strings.TrimSpace(request.Endpoint) == "" || len(request.Options) == 0 || !json.Valid(request.Options) {
		return platformadminapi.DatabaseConnection{}, platformadminapi.ErrInvalid
	}
	payload := struct {
		Name string `json:"name"`
		platformadminapi.PutDatabaseConnectionRequest
	}{Name: name, PutDatabaseConnectionRequest: request}
	var response platformadminapi.DatabaseConnection
	err := s.call(ctx, p, target, platformadminapi.DatabaseCapability, "define", true, payload, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) DeleteDatabaseConnection(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, name string) error {
	if err := validResourceName(name, 160); err != nil {
		return platformadminapi.ErrInvalid
	}
	return s.call(ctx, p, target, platformadminapi.DatabaseCapability, "remove", true, map[string]string{"name": name}, nil)
}

func (s *CapabilityPlatformAdminService) ProbeDatabaseConnection(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, name string) (platformadminapi.DatabaseProbe, error) {
	if err := validResourceName(name, 160); err != nil {
		return platformadminapi.DatabaseProbe{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.DatabaseProbe
	err := s.call(ctx, p, target, platformadminapi.DatabaseCapability, "probe", true, map[string]string{"name": name}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ArtifactRepositoryStatus(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) (platformadminapi.ArtifactRepositoryStatus, error) {
	var response platformadminapi.ArtifactRepositoryStatus
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, "status", false, struct{}{}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ListArtifactCatalog(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, query platformadminapi.ArtifactCatalogQuery) (platformadminapi.ArtifactCatalogPage, error) {
	if query.Page < 1 || query.PageSize < 1 || query.PageSize > 100 {
		return platformadminapi.ArtifactCatalogPage{}, platformadminapi.ErrInvalid
	}
	for _, value := range []string{query.PluginID, query.PluginPrefix, query.Namespace, query.Publisher, query.Version, query.Channel} {
		if len(value) > 160 || strings.ContainsAny(value, "\x00\r\n") {
			return platformadminapi.ArtifactCatalogPage{}, platformadminapi.ErrInvalid
		}
	}
	if query.Target != "" && query.Target != "backend" && query.Target != "frontend" && query.Target != "runner" && query.Target != "mobile" {
		return platformadminapi.ArtifactCatalogPage{}, platformadminapi.ErrInvalid
	}
	if query.Lifecycle != "" && query.Lifecycle != "active" && query.Lifecycle != "deprecated" && query.Lifecycle != "yanked" && query.Lifecycle != "revoked" {
		return platformadminapi.ArtifactCatalogPage{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.ArtifactCatalogPage
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, "listCatalog", false, query, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ArtifactRepositoryCapacity(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) (platformadminapi.ArtifactCapacity, error) {
	var response platformadminapi.ArtifactCapacity
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, "capacity", false, struct{}{}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ListArtifactReferences(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) (platformadminapi.ArtifactReferencePage, error) {
	var response platformadminapi.ArtifactReferencePage
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, "listReferences", false, struct{}{}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) PlanArtifactGarbageCollection(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) (platformadminapi.ArtifactGCPlan, error) {
	var response platformadminapi.ArtifactGCPlan
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, "gcPlan", false, struct{}{}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ArtifactGarbageCollectionStatus(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) (platformadminapi.ArtifactGCStatus, error) {
	var response platformadminapi.ArtifactGCStatus
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, "gcStatus", false, struct{}{}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) QuarantineArtifacts(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, request platformadminapi.QuarantineArtifactsRequest) (platformadminapi.ArtifactGCStatus, error) {
	if len(request.PlanID) != 64 || request.GraceHours < 24 || request.GraceHours > 24*365 {
		return platformadminapi.ArtifactGCStatus{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.ArtifactGCStatus
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, "gcQuarantine", true, request, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) SweepArtifacts(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) (platformadminapi.ArtifactGCStatus, error) {
	var response platformadminapi.ArtifactGCStatus
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, "gcSweep", true, struct{}{}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ArtifactMigrationStatus(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) (platformadminapi.ArtifactRepositoryMigration, error) {
	var response platformadminapi.ArtifactRepositoryMigration
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, "migrationStatus", false, struct{}{}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) SetArtifactLifecycle(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, request platformadminapi.ArtifactLifecycleRequest) (platformadminapi.ArtifactLifecycleResult, error) {
	if request.ExpectedRevision == 0 || validResourceName(request.Ref.PluginID, 160) != nil || request.Ref.Version == "" || len(request.Ref.Version) > 128 || validResourceName(request.Ref.Channel, 64) != nil || request.Reason == "" || len([]rune(request.Reason)) > 500 {
		return platformadminapi.ArtifactLifecycleResult{}, platformadminapi.ErrInvalid
	}
	switch request.Status {
	case "active", "deprecated", "yanked", "revoked":
	default:
		return platformadminapi.ArtifactLifecycleResult{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.ArtifactLifecycleResult
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, "setLifecycle", true, request, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) PrepareArtifactMigration(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, request platformadminapi.PrepareArtifactMigrationRequest) (platformadminapi.ArtifactRepositoryMigration, error) {
	if validResourceName(request.MigrationID, 96) != nil || validResourceName(request.TargetProvider, 160) != nil || validResourceName(request.TargetVolumeID, 80) != nil {
		return platformadminapi.ArtifactRepositoryMigration{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.ArtifactRepositoryMigration
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, "prepareMigration", true, request, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) artifactMigrationAction(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, migrationID, operation string, extra map[string]any) (platformadminapi.ArtifactRepositoryMigration, error) {
	if validResourceName(migrationID, 96) != nil {
		return platformadminapi.ArtifactRepositoryMigration{}, platformadminapi.ErrInvalid
	}
	payload := map[string]any{"migrationId": migrationID}
	for key, value := range extra {
		payload[key] = value
	}
	var response platformadminapi.ArtifactRepositoryMigration
	err := s.call(ctx, p, target, platformadminapi.ArtifactsCapability, operation, true, payload, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) SyncArtifactMigration(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id string) (platformadminapi.ArtifactRepositoryMigration, error) {
	return s.artifactMigrationAction(ctx, p, target, id, "syncMigration", nil)
}

func (s *CapabilityPlatformAdminService) CutoverArtifactMigration(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id string, request platformadminapi.CutoverArtifactMigrationRequest) (platformadminapi.ArtifactRepositoryMigration, error) {
	if request.ObservationSeconds < 60 || request.ObservationSeconds > 7*24*60*60 {
		return platformadminapi.ArtifactRepositoryMigration{}, platformadminapi.ErrInvalid
	}
	return s.artifactMigrationAction(ctx, p, target, id, "cutoverMigration", map[string]any{"observationSeconds": request.ObservationSeconds})
}

func (s *CapabilityPlatformAdminService) RollbackArtifactMigration(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id string) (platformadminapi.ArtifactRepositoryMigration, error) {
	return s.artifactMigrationAction(ctx, p, target, id, "rollbackMigration", nil)
}

func (s *CapabilityPlatformAdminService) FinalizeArtifactMigration(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id string) (platformadminapi.ArtifactRepositoryMigration, error) {
	return s.artifactMigrationAction(ctx, p, target, id, "finalizeMigration", nil)
}

func (s *CapabilityPlatformAdminService) ReleaseArtifactMigration(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id string) (platformadminapi.ArtifactRepositoryMigration, error) {
	return s.artifactMigrationAction(ctx, p, target, id, "releaseMigration", nil)
}

func (s *CapabilityPlatformAdminService) ListManagedNodes(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) ([]platformadminapi.ManagedNode, error) {
	var response struct {
		Items []platformadminapi.ManagedNode `json:"items"`
	}
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "listNodes", false, struct{}{}, &response)
	return response.Items, err
}

func (s *CapabilityPlatformAdminService) PutManagedNode(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id string, request platformadminapi.PutManagedNodeRequest) (platformadminapi.ManagedNode, error) {
	if err := validResourceName(id, 128); err != nil || request.Plan.Node.ID != id || request.Plan.Node.Tenant != p.TenantID || request.Plan.Validate() != nil {
		return platformadminapi.ManagedNode{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.ManagedNode
	payload := struct {
		ID        string             `json:"id"`
		Plan      nodebootstrap.Plan `json:"plan"`
		IfVersion *int64             `json:"ifVersion,omitempty"`
	}{ID: id, Plan: request.Plan, IfVersion: request.IfVersion}
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "putNode", true, payload, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ListBootstrapJobs(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) ([]platformadminapi.BootstrapJob, error) {
	var response struct {
		Items []platformadminapi.BootstrapJob `json:"items"`
	}
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "listBootstrapJobs", false, struct{}{}, &response)
	return response.Items, err
}

func (s *CapabilityPlatformAdminService) CreateBootstrapJob(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, nodeID string) (platformadminapi.BootstrapJob, error) {
	if err := validResourceName(nodeID, 128); err != nil {
		return platformadminapi.BootstrapJob{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.BootstrapJob
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "createBootstrap", true, map[string]string{"nodeId": nodeID}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ApproveBootstrapJob(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, jobID string) (platformadminapi.BootstrapJob, error) {
	if err := validResourceName(jobID, 128); err != nil {
		return platformadminapi.BootstrapJob{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.BootstrapJob
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "approveBootstrap", true, map[string]string{"jobId": jobID}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ListDeploymentTargets(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) ([]platformadminapi.DeploymentTarget, error) {
	var response struct {
		Items []platformadminapi.DeploymentTarget `json:"items"`
	}
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "listDeploymentTargets", false, struct{}{}, &response)
	return response.Items, err
}

func (s *CapabilityPlatformAdminService) ListServiceRevisions(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) ([]platformadminapi.ServiceRevision, error) {
	var response struct {
		Items []platformadminapi.ServiceRevision `json:"items"`
	}
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "listServiceRevisions", false, struct{}{}, &response)
	return response.Items, err
}

func (s *CapabilityPlatformAdminService) CreateServiceDraft(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, request platformadminapi.ServiceCompositionRequest) (platformadminapi.ServiceRevision, error) {
	var response platformadminapi.ServiceRevision
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "createServiceDraft", true, request, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) UpdateServiceDraft(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id uint64, request platformadminapi.ServiceCompositionRequest) (platformadminapi.ServiceRevision, error) {
	var response platformadminapi.ServiceRevision
	payload := struct {
		RevisionID uint64 `json:"revisionId"`
		platformadminapi.ServiceCompositionRequest
	}{RevisionID: id, ServiceCompositionRequest: request}
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "updateServiceDraft", true, payload, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) SubmitServiceDraft(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id uint64) (platformadminapi.ServiceRevision, error) {
	return s.serviceRevisionAction(ctx, p, target, id, "submitServiceDraft")
}

func (s *CapabilityPlatformAdminService) ApproveServiceRevision(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id uint64) (platformadminapi.ServiceRevision, error) {
	return s.serviceRevisionAction(ctx, p, target, id, "approveServiceRevision")
}

func (s *CapabilityPlatformAdminService) PublishServiceRevision(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id uint64) (platformadminapi.ServiceRevision, error) {
	return s.serviceRevisionAction(ctx, p, target, id, "publishServiceRevision")
}

func (s *CapabilityPlatformAdminService) RollbackServiceRevision(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id uint64) (platformadminapi.ServiceRevision, error) {
	return s.serviceRevisionAction(ctx, p, target, id, "rollbackServiceRevision")
}

func (s *CapabilityPlatformAdminService) ListServiceRevisionAudit(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id uint64) ([]platformadminapi.ServiceAuditEvent, error) {
	var response struct {
		Items []platformadminapi.ServiceAuditEvent `json:"items"`
	}
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "listServiceRevisionAudit", false, map[string]uint64{"revisionId": id}, &response)
	return response.Items, err
}

func (s *CapabilityPlatformAdminService) serviceRevisionAction(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id uint64, operation string) (platformadminapi.ServiceRevision, error) {
	if id == 0 {
		return platformadminapi.ServiceRevision{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.ServiceRevision
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, operation, true, map[string]uint64{"revisionId": id}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ListTestTargetBindings(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) ([]platformadminapi.TestTargetBinding, error) {
	var response struct {
		Items []platformadminapi.TestTargetBinding `json:"items"`
	}
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "listTestTargetBindings", false, struct{}{}, &response)
	return response.Items, err
}

func (s *CapabilityPlatformAdminService) PutTestTargetBinding(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id string, request platformadminapi.PutTestTargetBindingRequest) (platformadminapi.TestTargetBinding, error) {
	if err := validResourceName(id, 128); err != nil {
		return platformadminapi.TestTargetBinding{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.TestTargetBinding
	payload := struct {
		ID      string                                       `json:"id"`
		Binding platformadminapi.PutTestTargetBindingRequest `json:"binding"`
	}{ID: id, Binding: request}
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "putTestTargetBinding", true, payload, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ListTestReleases(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget) ([]platformadminapi.TestRelease, error) {
	var response struct {
		Items []platformadminapi.TestRelease `json:"items"`
	}
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "listTestReleases", false, struct{}{}, &response)
	return response.Items, err
}

func (s *CapabilityPlatformAdminService) CreateTestRelease(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, request platformadminapi.CreateTestReleaseRequest) (platformadminapi.TestRelease, error) {
	var response platformadminapi.TestRelease
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "createTestRelease", true, struct {
		Release platformadminapi.CreateTestReleaseRequest `json:"release"`
	}{Release: request}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) RollbackTestRelease(ctx context.Context, p portalapi.Principal, target portalapi.ManagementTarget, id uint64) (platformadminapi.TestRelease, error) {
	if id == 0 {
		return platformadminapi.TestRelease{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.TestRelease
	err := s.call(ctx, p, target, platformadminapi.DeploymentCapability, "rollbackTestRelease", true, map[string]uint64{"releaseId": id}, &response)
	return response, err
}

func validResourceName(value string, max int) error {
	if strings.TrimSpace(value) == "" || len(value) > max || strings.ContainsAny(value, "/\\\x00") {
		return platformadminapi.ErrInvalid
	}
	return nil
}
