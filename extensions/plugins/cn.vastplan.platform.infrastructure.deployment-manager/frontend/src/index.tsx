import { createBrowserPlatformAdminClient } from "@vastplan/platform-admin";
import { managementServicesFor, type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";
import { createDeploymentPage } from "./deployment-page.js";
import { createPluginInstallationPage } from "./installation-page.js";
import { messages } from "./localization.js";

const deploymentManagerPlugin = {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.deployment");
    const repositories = managementServicesFor(context.portal, "platform.artifacts.repository");
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
      const pluginTitle = context.i18n.message(
        services.length === 1 ? "installation.page.title" : "installation.page.titleService",
        services.length === 1 ? "服务插件" : "服务插件 · {service}",
        { service: service.label ?? service.id },
      );
      const repository = repositories[0] === undefined ? undefined : createBrowserPlatformAdminClient(context.portal.id, repositories[0].id);
      context.addCollectionPage(createPluginInstallationPage(client, repository, context.portal.id, service.id, `/operations/plugins${suffix}`, pluginTitle));
    }
  },
  localization: { defaultLocale: "zh-CN", messages },
};

export { createDeploymentPage } from "./deployment-page.js";
export { createPluginInstallationPage, installationRow } from "./installation-page.js";
export { buildInstallationRequests, installationTargetChoices } from "./installation-form.js";
export { buildBackendIntent, intentEditorValue, serviceIntentSchema } from "./intent-form.js";
export { dependencyGraphContent, deploymentRow, resolutionContent } from "./resolution-view.js";
export default deploymentManagerPlugin;
