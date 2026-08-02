package portalapi

import (
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

const (
	PreparePluginInstallationOperation  = "preparePluginInstallation"
	CommitPluginInstallationOperation   = "commitPluginInstallation"
	AbortPluginInstallationOperation    = "abortPluginInstallation"
	RollbackPluginInstallationOperation = "rollbackPluginInstallation"
)

type PluginInstallationStatus string

const (
	PluginInstallationPreparing  PluginInstallationStatus = "Preparing"
	PluginInstallationPrepared   PluginInstallationStatus = "Prepared"
	PluginInstallationCommitted  PluginInstallationStatus = "Committed"
	PluginInstallationAborted    PluginInstallationStatus = "Aborted"
	PluginInstallationRolledBack PluginInstallationStatus = "RolledBack"
)

// PluginInstallationRequest is accepted only from the trusted Deployment
// Manager workload. PortalID is explicit so one backend change never spreads
// implicitly to every Portal bound to the service.
type PluginInstallationRequest struct {
	CandidateID string                    `json:"candidateId"`
	PortalID    string                    `json:"portalId"`
	Action      plugininstallation.Action `json:"action"`
	PluginID    string                    `json:"pluginId"`
	Artifact    *pluginv1.ArtifactRef     `json:"artifact,omitempty"`
}

type PluginInstallationLookup struct {
	CandidateID string `json:"candidateId"`
	PortalID    string `json:"portalId"`
}

type PluginInstallationPreparation struct {
	CandidateID          string                       `json:"candidateId"`
	PortalID             string                       `json:"portalId"`
	Status               PluginInstallationStatus     `json:"status"`
	PluginID             string                       `json:"pluginId"`
	Action               plugininstallation.Action    `json:"action"`
	Artifact             *pluginv1.ArtifactRef        `json:"artifact,omitempty"`
	VersionID            uint64                       `json:"versionId"`
	PreviousActivationID uint64                       `json:"previousActivationId"`
	ActivationID         uint64                       `json:"activationId,omitempty"`
	ArtifactReferences   []pluginv1.ArtifactReference `json:"artifactReferences,omitempty"`
	UpdatedAt            string                       `json:"updatedAt"`
}
