package platformadminapi

import (
	"context"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

type Service interface {
	ListSettings(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) ([]Setting, error)
	PutSetting(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, PutSettingRequest) (Setting, error)
	DeleteSetting(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, *int64) error
	ListCredentials(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) ([]CredentialMetadata, error)
	PutCredential(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, PutCredentialRequest) (CredentialMetadata, error)
	RotateCredential(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (CredentialMetadata, error)
	RevokeCredential(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (CredentialMetadata, error)
	ListDatabaseConnections(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]DatabaseConnection, error)
	PutDatabaseConnection(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, PutDatabaseConnectionRequest) (DatabaseConnection, error)
	DeleteDatabaseConnection(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) error
	ProbeDatabaseConnection(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (DatabaseProbe, error)
	ArtifactRepositoryStatus(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactRepositoryStatus, error)
	ListArtifactCatalog(context.Context, portalapi.Principal, portalapi.ManagementTarget, ArtifactCatalogQuery) (ArtifactCatalogPage, error)
	ArtifactRepositoryCapacity(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactCapacity, error)
	ListArtifactReferences(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactReferencePage, error)
	PlanArtifactGarbageCollection(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactGCPlan, error)
	ArtifactGarbageCollectionStatus(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactGCStatus, error)
	QuarantineArtifacts(context.Context, portalapi.Principal, portalapi.ManagementTarget, QuarantineArtifactsRequest) (ArtifactGCStatus, error)
	SweepArtifacts(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactGCStatus, error)
	SetArtifactLifecycle(context.Context, portalapi.Principal, portalapi.ManagementTarget, ArtifactLifecycleRequest) (ArtifactLifecycleResult, error)
	ArtifactMigrationStatus(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactRepositoryMigration, error)
	PrepareArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, PrepareArtifactMigrationRequest) (ArtifactRepositoryMigration, error)
	SyncArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (ArtifactRepositoryMigration, error)
	CutoverArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, CutoverArtifactMigrationRequest) (ArtifactRepositoryMigration, error)
	RollbackArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (ArtifactRepositoryMigration, error)
	FinalizeArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (ArtifactRepositoryMigration, error)
	ReleaseArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (ArtifactRepositoryMigration, error)
	ListManagedNodes(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]ManagedNode, error)
	PutManagedNode(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, PutManagedNodeRequest) (ManagedNode, error)
	ListBootstrapJobs(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]BootstrapJob, error)
	CreateBootstrapJob(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (BootstrapJob, error)
	ApproveBootstrapJob(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (BootstrapJob, error)
	ListDeploymentTargets(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]DeploymentTarget, error)
	ListServiceRevisions(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]ServiceRevision, error)
	CreateIntentDraft(context.Context, portalapi.Principal, portalapi.ManagementTarget, ServiceIntentRequest) (ServiceRevision, error)
	UpdateIntentDraft(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64, ServiceIntentRequest) (ServiceRevision, error)
	RefreshIntentDraft(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) (ServiceRevision, error)
	SubmitServiceDraft(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) (ServiceRevision, error)
	ApproveServiceRevision(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) (ServiceRevision, error)
	PublishServiceRevision(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) (ServiceRevision, error)
	RollbackServiceRevision(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) (ServiceRevision, error)
	ListServiceRevisionAudit(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) ([]ServiceAuditEvent, error)
	ListTestTargetBindings(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]TestTargetBinding, error)
	PutTestTargetBinding(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, PutTestTargetBindingRequest) (TestTargetBinding, error)
	ListTestReleases(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]TestRelease, error)
	CreateTestRelease(context.Context, portalapi.Principal, portalapi.ManagementTarget, CreateTestReleaseRequest) (TestRelease, error)
	RollbackTestRelease(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) (TestRelease, error)
}
