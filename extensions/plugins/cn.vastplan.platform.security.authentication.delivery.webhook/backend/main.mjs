import { Contribution, Plugin, callResult } from "@vastplan/backend-plugin";
import { MaterialLeaseClient } from "@vastplan/credential-lease-node";
import { loadConfiguration } from "./config.mjs";
import { WebhookDelivery } from "./delivery.mjs";

const plugin = new Plugin({ id:"cn.vastplan.platform.security.authentication.delivery.webhook", version:"0.1.0", engines:{backend:"^0.1"} });
const delivery = new WebhookDelivery(loadConfiguration(), { materialLease:new MaterialLeaseClient(plugin, {audience:process.env.VASTPLAN_RUNTIME_AUDIENCE}) });
const handler = (operation) => async (invocation, _host, context, payload) => {
  invocation.throwIfCancelled();
  try {
    const value = operation === "health" ? delivery.health() : await delivery.deliver(JSON.parse(payload.toString("utf8")), context, invocation.signal.signal);
    return callResult.ok(Buffer.from(JSON.stringify(value)));
  } catch { return callResult.error("foundation.authentication.delivery.unavailable", "Authentication Delivery 不可用"); }
};
plugin.contribute(new Contribution({ extensionPoint:"tool.package", id:"foundation.security.authentication.delivery", descriptor:{title:"企业认证验证码投递",subcommands:[{name:"deliver",description:"投递一次性验证码"},{name:"health",description:"检查投递服务"}]}, handlers:{deliver:handler("deliver"),health:handler("health")} }));

export const start = () => plugin.serve();
export const shutdown = () => plugin.shutdown();
