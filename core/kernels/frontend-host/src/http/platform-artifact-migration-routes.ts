import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole, resourceName } from "./platform-route-contract";
import { requireEmptyJSONObject, requireJSONObject, withRequestJSON } from "./request-json";

const capability = "platform.artifacts.repository";
const actions: Readonly<Record<string, string>> = Object.freeze({ sync: "syncMigration", cutover: "cutoverMigration", rollback: "rollbackMigration", finalize: "finalizeMigration", release: "releaseMigration" });

export class PlatformArtifactMigrationRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    let operation: string | undefined;
    let migrationId: string | undefined;
    if (parts.length === 1 && parts[0] === "migrations") operation = "prepareMigration";
    else if (parts.length === 3 && parts[0] === "migrations") { migrationId = resourceName(parts[1], 96); operation = actions[parts[2]!]; }
    else return false;
    if (migrationId === undefined && parts.length === 3) { sendAPIError(response, 400, "invalid_migration_id"); return true; }
    if (operation === undefined) { sendAPIError(response, 404, "not_found"); return true; }
    if (!authorizePlatformOperation(this.client, target, capability, operation, true, response) || !requirePlatformRole(principal, "platform.artifacts.migrate", response)) return true;
    if (request.method !== "POST") { sendAPIError(response, 405, "method_not_allowed"); return true; }
    await withRequestJSON(request, response, async (body) => {
      const payload = operation === "prepareMigration" ? body : { ...(operation === "cutoverMigration" ? requireJSONObject(body) : requireEmptyJSONObject(body)), migrationId };
      await sendPlatformResponse({ client: this.client, principal, target, capability, operation, write: true, payload, response, signal });
    });
    return true;
  }
}
