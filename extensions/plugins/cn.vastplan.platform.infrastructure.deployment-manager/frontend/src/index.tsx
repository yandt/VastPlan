import { createBrowserPlatformAdminClient } from "@vastplan/platform-admin";
import { managementServicesFor, type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";
import { createDeploymentPage } from "./deployment-page.js";
import { messages } from "./localization.js";

const deploymentManagerPlugin = {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.deployment");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.deployment 服务");
    for (const service of services) {
      const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
      const suffix = services.length === 1 ? "" : `/${service.id}`;
      const title = context.i18n.message(
        services.length === 1 ? "page.title" : "page.titleService",
        services.length === 1 ? "服务与节点部署" : "服务与节点部署 · {service}",
        { service: service.label ?? service.id },
      );
      context.addCollectionPage(createDeploymentPage(client, service.id, `/settings/deployment${suffix}`, title));
    }
  },
  localization: { defaultLocale: "zh-CN", messages },
};

export { createDeploymentPage } from "./deployment-page.js";
export { buildBackendIntent, intentEditorValue, serviceIntentSchema } from "./intent-form.js";
export { dependencyGraphContent, deploymentRow, resolutionContent } from "./resolution-view.js";
export default deploymentManagerPlugin;
