import { managementAllows, type ManagedService } from "./management-binding";

const operations = Object.freeze({
  "platform.settings": new Set(["list", "put", "delete"]),
  "platform.credentials": new Set(["list", "put", "rotate", "revoke"]),
  "platform.database": new Set(["list", "define", "remove", "probe"]),
  "platform.artifacts.repository": new Set(["status", "capacity", "listCatalog", "listPublishJournal", "resolve", "listReferences", "setLifecycle", "gcPlan", "gcStatus", "gcQuarantine", "gcSweep", "migrationStatus", "prepareMigration", "syncMigration", "cutoverMigration", "rollbackMigration", "finalizeMigration", "releaseMigration"]),
  "platform.deployment": new Set(["listNodes", "putNode", "listBootstrapJobs", "createBootstrap", "approveBootstrap", "listDeploymentTargets", "listServiceRevisions", "createServiceDraft", "updateServiceDraft", "submitServiceDraft", "approveServiceRevision", "publishServiceRevision", "rollbackServiceRevision", "listServiceRevisionAudit", "listTestTargetBindings", "putTestTargetBinding", "listTestReleases", "createTestRelease", "rollbackTestRelease"]),
});

export type PlatformCapability = keyof typeof operations;

export function platformOperationAllowed(service: ManagedService, capability: string, operation: string, write: boolean): capability is PlatformCapability {
  return capability in operations && operations[capability as PlatformCapability].has(operation) && managementAllows(service, capability, operation, write);
}
