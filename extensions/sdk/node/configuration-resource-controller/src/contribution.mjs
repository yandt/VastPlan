import { Contribution, callResult } from "@vastplan/backend-plugin";

import { CONFIGURATION_RESOURCE_EXTENSION_POINT, CONFIGURATION_RESOURCE_PROTOCOL, configurationResourceControllerCapability } from "./identities.mjs";
import { parseResourceControllerRequest } from "./requests.mjs";
import { validateResourceControllerResponse } from "./responses.mjs";

const operations = ["list", "get", "prepare", "commit", "abort", "status"];

export function configurationResourceControllerContribution(pluginId, controller) {
  if (!controller || operations.some((operation) => typeof controller[operation] !== "function")) throw new Error("configuration.resource.v1 controller 必须实现 list/get/prepare/commit/abort/status");
  return new Contribution({
    extensionPoint: CONFIGURATION_RESOURCE_EXTENSION_POINT,
    id: configurationResourceControllerCapability(pluginId),
    descriptor: { protocol: CONFIGURATION_RESOURCE_PROTOCOL },
    handlers: Object.fromEntries(operations.map((operation) => [operation, handler(operation, controller)])),
  });
}

function handler(operation, controller) {
  return async (invocation, host, context, payload) => {
    const caller = context?.caller;
    if (!context?.tenant_id || !caller || ![3, "CALLER_KIND_PLUGIN"].includes(caller.kind) || caller.id !== "cn.vastplan.platform.configuration.plugin-settings") {
      return callResult.error("configuration.resource.permission_denied", "configuration.resource.v1 只接受 plugin-settings 认证调用");
    }
    let request;
    try { request = parseResourceControllerRequest(operation, payload); }
    catch (error) { return callResult.error("configuration.resource.invalid_request", error.message); }
    try {
      invocation.throwIfCancelled();
      const response = validateResourceControllerResponse(operation, await controller[operation](request, { invocation, host, context }));
      return callResult.ok(Buffer.from(JSON.stringify(response)));
    } catch (error) {
      return callResult.error("configuration.resource.rejected", error.message);
    }
  };
}
