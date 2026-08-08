import { createBrowserPlatformAdminClient } from "@vastplan/platform-admin";
import { managementServicesFor, type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";
import { createMarketPage } from "./market-page.js";
import { createTaskPage } from "./task-page.js";
import { messages } from "./localization.js";

const plugin = {
  register(context: WorkbenchFrontendPluginContext) {
    const markets = managementServicesFor(context.portal, "platform.artifacts.marketplace");
    const deployments = managementServicesFor(context.portal, "platform.deployment").filter((service) => service.resource?.kind === "service-unit");
    if (markets.length !== 1 || deployments.length !== 1) return;
    const marketplace = createBrowserPlatformAdminClient(context.portal.id, markets[0].id);
    const deployment = createBrowserPlatformAdminClient(context.portal.id, deployments[0].id);
    context.addCollectionPage(createMarketPage(marketplace, deployment, "/operations/service-plugins/marketplace", context.i18n.message, markets[0].id));
    context.addCollectionPage(createTaskPage(deployment, "/operations/service-plugins/tasks", context.i18n.message, markets[0].id));
  },
  localization: { defaultLocale: "zh-CN", messages },
};

export { createMarketPage } from "./market-page.js";
export { createTaskPage } from "./task-page.js";
export default plugin;
