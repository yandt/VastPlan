import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { PlatformArtifactGCRoutes } from "./platform-artifact-gc-routes";
import { PlatformArtifactLifecycleRoutes } from "./platform-artifact-lifecycle-routes";
import { PlatformArtifactMigrationRoutes } from "./platform-artifact-migration-routes";
import { PlatformArtifactPublicationRoutes } from "./platform-artifact-publication-routes";
import { PlatformArtifactQueryRoutes } from "./platform-artifact-query-routes";
import { PlatformArtifactAssessmentRoutes } from "./platform-artifact-assessment-routes";

export class PlatformArtifactRoutes {
  private readonly queries: PlatformArtifactQueryRoutes;
  private readonly lifecycle: PlatformArtifactLifecycleRoutes;
  private readonly gc: PlatformArtifactGCRoutes;
  private readonly migrations: PlatformArtifactMigrationRoutes;
  private readonly publications: PlatformArtifactPublicationRoutes;
  private readonly assessments: PlatformArtifactAssessmentRoutes;

  public constructor(client: PlatformCapabilityPort) {
    this.queries = new PlatformArtifactQueryRoutes(client);
    this.lifecycle = new PlatformArtifactLifecycleRoutes(client);
    this.gc = new PlatformArtifactGCRoutes(client);
    this.migrations = new PlatformArtifactMigrationRoutes(client);
    this.publications = new PlatformArtifactPublicationRoutes(client);
    this.assessments = new PlatformArtifactAssessmentRoutes(client);
  }

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "artifacts") return false;
    const artifactParts = parts.slice(1);
    if (await this.assessments.handle(artifactParts, principal, target, request, response, signal)) return true;
    if (await this.queries.handle(artifactParts, principal, target, request, response, signal)) return true;
    if (await this.lifecycle.handle(artifactParts, principal, target, request, response, signal)) return true;
    if (await this.gc.handle(artifactParts, principal, target, request, response, signal)) return true;
    if (await this.migrations.handle(artifactParts, principal, target, request, response, signal)) return true;
    if (await this.publications.handle(artifactParts, principal, target, request, response, signal)) return true;
    sendAPIError(response, 404, "not_found", request.method === "HEAD");
    return true;
  }
}
