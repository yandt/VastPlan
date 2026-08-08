package portalapi

import frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"

const (
	NavigationOrganizerPluginID              = "cn.vastplan.application.portal.navigation-organizer"
	ReadNavigationConfigurationOperation     = "readNavigationConfiguration"
	PrepareNavigationConfigurationOperation  = "prepareNavigationConfiguration"
	CommitNavigationConfigurationOperation   = "commitNavigationConfiguration"
	AbortNavigationConfigurationOperation    = "abortNavigationConfiguration"
	RollbackNavigationConfigurationOperation = "rollbackNavigationConfiguration"
)

type NavigationConfigurationStatus string

const (
	NavigationConfigurationPreparing  NavigationConfigurationStatus = "Preparing"
	NavigationConfigurationPrepared   NavigationConfigurationStatus = "Prepared"
	NavigationConfigurationCommitted  NavigationConfigurationStatus = "Committed"
	NavigationConfigurationAborted    NavigationConfigurationStatus = "Aborted"
	NavigationConfigurationRolledBack NavigationConfigurationStatus = "RolledBack"
)

type NavigationConfigurationSnapshot struct {
	PortalID     string                                   `json:"portalId"`
	ServiceID    string                                   `json:"serviceId"`
	ActivationID uint64                                   `json:"activationId"`
	Folders      []frontendcompositionv1.NavigationFolder `json:"folders"`
}

type NavigationConfigurationRequest struct {
	CandidateID          string                                   `json:"candidateId"`
	PortalID             string                                   `json:"portalId"`
	ServiceID            string                                   `json:"serviceId"`
	ExpectedActivationID uint64                                   `json:"expectedActivationId"`
	Folders              []frontendcompositionv1.NavigationFolder `json:"folders"`
}

type NavigationConfigurationLookup struct {
	CandidateID string `json:"candidateId"`
	PortalID    string `json:"portalId"`
	ServiceID   string `json:"serviceId"`
}

type NavigationConfigurationPreparation struct {
	CandidateID          string                        `json:"candidateId"`
	PortalID             string                        `json:"portalId"`
	ServiceID            string                        `json:"serviceId"`
	Status               NavigationConfigurationStatus `json:"status"`
	RequestDigest        string                        `json:"requestDigest"`
	ConfigurationDigest  string                        `json:"configurationDigest"`
	VersionID            uint64                        `json:"versionId"`
	PreviousActivationID uint64                        `json:"previousActivationId"`
	ActivationID         uint64                        `json:"activationId,omitempty"`
	UpdatedAt            string                        `json:"updatedAt"`
}
