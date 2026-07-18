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
	Call(context.Context, portalapi.Principal, string, string, []byte) ([]byte, error)
}

type CapabilityPlatformAdminService struct{ caller PlatformCapabilityCaller }

func NewCapabilityPlatformAdminService(caller PlatformCapabilityCaller) (*CapabilityPlatformAdminService, error) {
	if caller == nil {
		return nil, errors.New("平台管理 capability caller 不能为空")
	}
	return &CapabilityPlatformAdminService{caller: caller}, nil
}

func (s *CapabilityPlatformAdminService) call(ctx context.Context, p portalapi.Principal, capability, operation string, request, response any) error {
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	raw, err = s.caller.Call(ctx, p, capability, operation, raw)
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

func (s *CapabilityPlatformAdminService) ListSettings(ctx context.Context, p portalapi.Principal, prefix string) ([]platformadminapi.Setting, error) {
	var response struct {
		Items []platformadminapi.Setting `json:"items"`
	}
	err := s.call(ctx, p, platformadminapi.SettingsCapability, "list", map[string]string{"prefix": prefix}, &response)
	return response.Items, err
}

func (s *CapabilityPlatformAdminService) PutSetting(ctx context.Context, p portalapi.Principal, key string, request platformadminapi.PutSettingRequest) (platformadminapi.Setting, error) {
	if err := validResourceName(key, 320); err != nil || len(request.Value) == 0 || !json.Valid(request.Value) {
		return platformadminapi.Setting{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.Setting
	payload := struct {
		Key       string          `json:"key"`
		Value     json.RawMessage `json:"value"`
		IfVersion *int64          `json:"ifVersion,omitempty"`
	}{Key: key, Value: request.Value, IfVersion: request.IfVersion}
	if err := s.call(ctx, p, platformadminapi.SettingsCapability, "put", payload, &response); err != nil {
		return response, err
	}
	response.Value = append(json.RawMessage(nil), request.Value...)
	return response, nil
}

func (s *CapabilityPlatformAdminService) DeleteSetting(ctx context.Context, p portalapi.Principal, key string, ifVersion *int64) error {
	if err := validResourceName(key, 320); err != nil {
		return platformadminapi.ErrInvalid
	}
	return s.call(ctx, p, platformadminapi.SettingsCapability, "delete", struct {
		Key       string `json:"key"`
		IfVersion *int64 `json:"ifVersion,omitempty"`
	}{key, ifVersion}, nil)
}

func (s *CapabilityPlatformAdminService) ListCredentials(ctx context.Context, p portalapi.Principal, prefix string) ([]platformadminapi.CredentialMetadata, error) {
	var response []platformadminapi.CredentialMetadata
	err := s.call(ctx, p, platformadminapi.CredentialsCapability, "list", map[string]string{"prefix": prefix}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) PutCredential(ctx context.Context, p portalapi.Principal, name string, request platformadminapi.PutCredentialRequest) (platformadminapi.CredentialMetadata, error) {
	if err := validResourceName(name, 160); err != nil || request.Value == "" {
		return platformadminapi.CredentialMetadata{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.CredentialMetadata
	err := s.call(ctx, p, platformadminapi.CredentialsCapability, "put", map[string]string{"name": name, "value": request.Value}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) credentialAction(ctx context.Context, p portalapi.Principal, name, operation string) (platformadminapi.CredentialMetadata, error) {
	if err := validResourceName(name, 160); err != nil {
		return platformadminapi.CredentialMetadata{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.CredentialMetadata
	err := s.call(ctx, p, platformadminapi.CredentialsCapability, operation, map[string]string{"name": name}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) RotateCredential(ctx context.Context, p portalapi.Principal, name string) (platformadminapi.CredentialMetadata, error) {
	return s.credentialAction(ctx, p, name, "rotate")
}

func (s *CapabilityPlatformAdminService) RevokeCredential(ctx context.Context, p portalapi.Principal, name string) (platformadminapi.CredentialMetadata, error) {
	return s.credentialAction(ctx, p, name, "revoke")
}

func (s *CapabilityPlatformAdminService) ListDatabaseConnections(ctx context.Context, p portalapi.Principal) ([]platformadminapi.DatabaseConnection, error) {
	var response []platformadminapi.DatabaseConnection
	err := s.call(ctx, p, platformadminapi.DatabaseCapability, "list", struct{}{}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) PutDatabaseConnection(ctx context.Context, p portalapi.Principal, name string, request platformadminapi.DatabaseConnection) (platformadminapi.DatabaseConnection, error) {
	if err := validResourceName(name, 160); err != nil || request.Name != "" && request.Name != name || strings.TrimSpace(request.Driver) == "" || strings.TrimSpace(request.Endpoint) == "" || strings.TrimSpace(request.Credential) == "" {
		return platformadminapi.DatabaseConnection{}, platformadminapi.ErrInvalid
	}
	request.Name = name
	var response platformadminapi.DatabaseConnection
	err := s.call(ctx, p, platformadminapi.DatabaseCapability, "define", request, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) DeleteDatabaseConnection(ctx context.Context, p portalapi.Principal, name string) error {
	if err := validResourceName(name, 160); err != nil {
		return platformadminapi.ErrInvalid
	}
	return s.call(ctx, p, platformadminapi.DatabaseCapability, "remove", map[string]string{"name": name}, nil)
}

func (s *CapabilityPlatformAdminService) ProbeDatabaseConnection(ctx context.Context, p portalapi.Principal, name string) (platformadminapi.DatabaseProbe, error) {
	if err := validResourceName(name, 160); err != nil {
		return platformadminapi.DatabaseProbe{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.DatabaseProbe
	err := s.call(ctx, p, platformadminapi.DatabaseCapability, "probe", map[string]string{"name": name}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ArtifactRepositoryStatus(ctx context.Context, p portalapi.Principal) (platformadminapi.ArtifactRepositoryStatus, error) {
	var response platformadminapi.ArtifactRepositoryStatus
	err := s.call(ctx, p, platformadminapi.ArtifactsCapability, "status", struct{}{}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ListManagedNodes(ctx context.Context, p portalapi.Principal) ([]platformadminapi.ManagedNode, error) {
	var response struct {
		Items []platformadminapi.ManagedNode `json:"items"`
	}
	err := s.call(ctx, p, platformadminapi.DeploymentCapability, "listNodes", struct{}{}, &response)
	return response.Items, err
}

func (s *CapabilityPlatformAdminService) PutManagedNode(ctx context.Context, p portalapi.Principal, id string, request platformadminapi.PutManagedNodeRequest) (platformadminapi.ManagedNode, error) {
	if err := validResourceName(id, 128); err != nil || request.Plan.Node.ID != id || request.Plan.Node.Tenant != p.TenantID || request.Plan.Validate() != nil {
		return platformadminapi.ManagedNode{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.ManagedNode
	payload := struct {
		ID        string             `json:"id"`
		Plan      nodebootstrap.Plan `json:"plan"`
		IfVersion *int64             `json:"ifVersion,omitempty"`
	}{ID: id, Plan: request.Plan, IfVersion: request.IfVersion}
	err := s.call(ctx, p, platformadminapi.DeploymentCapability, "putNode", payload, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ListBootstrapJobs(ctx context.Context, p portalapi.Principal) ([]platformadminapi.BootstrapJob, error) {
	var response struct {
		Items []platformadminapi.BootstrapJob `json:"items"`
	}
	err := s.call(ctx, p, platformadminapi.DeploymentCapability, "listBootstrapJobs", struct{}{}, &response)
	return response.Items, err
}

func (s *CapabilityPlatformAdminService) CreateBootstrapJob(ctx context.Context, p portalapi.Principal, nodeID string) (platformadminapi.BootstrapJob, error) {
	if err := validResourceName(nodeID, 128); err != nil {
		return platformadminapi.BootstrapJob{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.BootstrapJob
	err := s.call(ctx, p, platformadminapi.DeploymentCapability, "createBootstrap", map[string]string{"nodeId": nodeID}, &response)
	return response, err
}

func (s *CapabilityPlatformAdminService) ApproveBootstrapJob(ctx context.Context, p portalapi.Principal, jobID string) (platformadminapi.BootstrapJob, error) {
	if err := validResourceName(jobID, 128); err != nil {
		return platformadminapi.BootstrapJob{}, platformadminapi.ErrInvalid
	}
	var response platformadminapi.BootstrapJob
	err := s.call(ctx, p, platformadminapi.DeploymentCapability, "approveBootstrap", map[string]string{"jobId": jobID}, &response)
	return response, err
}

func validResourceName(value string, max int) error {
	if strings.TrimSpace(value) == "" || len(value) > max || strings.ContainsAny(value, "/\\\x00") {
		return platformadminapi.ErrInvalid
	}
	return nil
}
