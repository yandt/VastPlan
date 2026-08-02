import { createBrowserManagementAPIClient, createBrowserPlatformAdminClient } from "@vastplan/platform-admin";
import {
  managementServicesFor,
  type WorkbenchFrontendPluginContext,
} from "@vastplan/workbench-sdk";
import { createAPIExposurePage } from "./page";
import { createDataPlaneExposurePage } from "./data-plane-page";
import { APIExposureManagementClient } from "./management-client";

export { createAPIExposurePage } from "./page";
export { createDataPlaneExposurePage } from "./data-plane-page";

export default {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.api-exposure");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.api-exposure 服务");
    for (const service of services) {
      const managementAPI = service.apis?.find((api) => api.id === "primary");
      if (managementAPI === undefined) throw new Error(`Portal 服务 ${service.id} 未绑定 API Exposure Management Contract`);
      context.addCollectionPage(createAPIExposurePage(
        new APIExposureManagementClient(createBrowserManagementAPIClient(context.portal.id, service.id, managementAPI.id)),
        service.id,
        services.length === 1 ? undefined : service.label ?? service.id,
      ));
      context.addCollectionPage(createDataPlaneExposurePage(
        createBrowserPlatformAdminClient(context.portal.id, service.id),
        service.id,
      ));
    }
  },
  localization: { defaultLocale: "zh-CN", messages: { "zh-CN": { "navigation.integration": "集成与 API" }, "en-US": { "navigation.integration": "Integration and APIs" } } },
};
