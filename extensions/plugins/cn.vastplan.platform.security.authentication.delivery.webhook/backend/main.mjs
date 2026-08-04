import { Contribution, Plugin, callResult } from "@vastplan/backend-plugin";
import { configurationResourceCollectionId, configurationResourceControllerContribution } from "@vastplan/configuration-resource-controller-node";
import { MaterialLeaseClient } from "@vastplan/credential-lease-node";
import { pluginId, profileCollectionKey } from "./config.mjs";
import { WebhookDelivery } from "./delivery.mjs";
import { WebhookProfileController } from "./profile-controller.mjs";
import { SharedProfileStateStore } from "./state-store.mjs";

const plugin = new Plugin({ id:pluginId, version:"0.3.0", engines:{backend:"^0.1"} });
const materialLease = new MaterialLeaseClient(plugin, {audience:process.env.VASTPLAN_RUNTIME_AUDIENCE});
const profiles = new Map();
const profileController = new WebhookProfileController({
  collectionId: configurationResourceCollectionId(pluginId, profileCollectionKey),
  store: new SharedProfileStateStore(plugin), materialLease, profiles,
});
const delivery = new WebhookDelivery(profiles, { materialLease });
const handler = (operation) => async (invocation, host, context, payload) => {
  invocation.throwIfCancelled();
  try {
    await profileController.ensure({ invocation, host, context });
    const value = operation === "health" ? profileController.health() : await delivery.deliver(JSON.parse(payload.toString("utf8")), context, invocation.signal.signal);
    return callResult.ok(Buffer.from(JSON.stringify(value)));
  } catch { return callResult.error("foundation.authentication.delivery.unavailable", "Authentication Delivery 不可用"); }
};
plugin.contribute(new Contribution({ extensionPoint:"tool.package", id:"foundation.security.authentication.delivery", descriptor:{title:"企业认证验证码投递",subcommands:[{name:"deliver",description:"向已解析企业主体投递一次性验证码"},{name:"health",description:"检查投递服务配置"}]}, handlers:{deliver:handler("deliver"),health:handler("health")} }));
plugin.contribute(configurationResourceControllerContribution(pluginId, profileController));

export const start = () => plugin.serve();
export const shutdown = () => plugin.shutdown();
