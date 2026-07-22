import { createBrowserPlatformAdminClient } from "@vastplan/platform-admin";
import {
  managementServicesFor,
  type WorkbenchFrontendPluginContext,
} from "@vastplan/workbench-sdk";
import { createAPIExposurePage } from "./page";
import { createDataPlaneExposurePage } from "./data-plane-page";

export { createAPIExposurePage } from "./page";
export { createDataPlaneExposurePage } from "./data-plane-page";

export default {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.api-exposure");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.api-exposure 服务");
    for (const service of services) {
      context.addCollectionPage(createAPIExposurePage(
        createBrowserPlatformAdminClient(context.portal.id, service.id),
        service.id,
        services.length === 1 ? undefined : service.label ?? service.id,
      ));
      context.addCollectionPage(createDataPlaneExposurePage(
        createBrowserPlatformAdminClient(context.portal.id, service.id),
        service.id,
      ));
    }
  },
  localization: { defaultLocale: "zh-CN", messages: { "zh-CN": {}, "en-US": {} } },
};
