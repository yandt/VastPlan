import { Contribution, callResult } from "@vastplan/backend-plugin";

import {
  CONFIGURATION_CONTROLLER_EXTENSION_POINT,
  CONFIGURATION_CONTROLLER_PROTOCOL,
  configurationControllerCapability,
  parseControllerRequest,
  validateObservation,
} from "./wire.mjs";

const operations = ["prepare", "commit", "abort", "status"];

export function configurationControllerContribution(pluginId, controller) {
  if (!controller || operations.some((operation) => typeof controller[operation] !== "function")) throw new Error("configuration.v1 controller 必须实现 prepare/commit/abort/status");
  return new Contribution({
    extensionPoint: CONFIGURATION_CONTROLLER_EXTENSION_POINT,
    id: configurationControllerCapability(pluginId),
    descriptor: { protocol: CONFIGURATION_CONTROLLER_PROTOCOL },
    handlers: Object.fromEntries(operations.map((operation) => [operation, handler(operation, controller)])),
  });
}

function handler(operation, controller) {
  return async (invocation, host, context, payload) => {
    const caller = context?.caller;
    if (!context?.tenant_id || !caller || ![3, "CALLER_KIND_PLUGIN"].includes(caller.kind) || caller.id !== "cn.vastplan.platform.configuration.plugin-settings") {
      return callResult.error("configuration.controller.permission_denied", "configuration.v1 只接受 plugin-settings 认证调用");
    }
    let request;
    try {
      request = parseControllerRequest(operation, payload);
    } catch (error) {
      return callResult.error("configuration.controller.invalid_request", error.message);
    }
    try {
      invocation.throwIfCancelled();
      const observation = validateObservation(await controller[operation](request, { invocation, host, context }));
      return callResult.ok(Buffer.from(JSON.stringify(observation)));
    } catch (error) {
      return callResult.error("configuration.controller.rejected", error.message);
    }
  };
}
