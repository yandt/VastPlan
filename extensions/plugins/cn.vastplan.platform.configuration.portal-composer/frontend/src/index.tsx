import { PortalControlClient, type PortalFetch } from "@vastplan/ui-primitives";
import { createBrowserPlatformAdminClient } from "@vastplan/platform-admin";
import { managementServicesFor, type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";
import { createPortalPage } from "./portal-workspace";

function createDefaultClient(): PortalControlClient {
  const fetcher: PortalFetch = (input, init) => globalThis.fetch(input, init as RequestInit);
  return new PortalControlClient({ fetch: fetcher });
}

export { buildPortalConfiguration, configurationToForm, portalConfigurationSchema, portalConfigurationSchemaWithPermissions } from "./portal-form";
export { createPortalPage } from "./portal-workspace";

export default {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.authorization");
    if (services.length !== 1) throw new Error("Portal 必须绑定唯一的 platform.authorization 服务，无法校验访问权限");
    const service = services[0];
    context.addCollectionPage(createPortalPage(createDefaultClient(), createBrowserPlatformAdminClient(context.portal.id, service.id)));
  },
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": {
        "navigation.portal-management": "Portal 管理",
        "page.title": "Portal 管理",
        "page.description": "管理工作副本、发布审批、上线记录和可选版本历史",
        "page.navigation": "Portal 管理",
        "form.audience": "访问权限",
        "form.audience.description": "限制可进入此 Portal 的权限代码；用户拥有任意一项即可访问，留空则不额外限制。",
        "form.audience.missing": "以下访问权限已不存在或不可分配：{permissions}",
      },
      "en-US": {
        "navigation.portal-management": "Portal Management",
        "page.title": "Portal Management",
        "page.description": "Manage working copies, publications, releases, and optional version history",
        "page.navigation": "Portal Management",
        "form.audience": "Access permissions",
        "form.audience.description": "Permission codes allowed to enter this Portal. Any matching permission grants access; leave empty for no additional restriction.",
        "form.audience.missing": "These access permissions no longer exist or cannot be assigned: {permissions}",
      },
    },
  },
};
