import { createBrowserPlatformAdminClient } from "@vastplan/platform-admin";
import {
  managementServicesFor,
  type WorkbenchFrontendPluginContext,
} from "@vastplan/workbench-sdk";
import { createAuthenticationProviderPage } from "./page.js";

export { createAuthenticationProviderPage } from "./page.js";

export default {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(
      context.portal,
      "foundation.security.authentication.providers",
    );
    if (services.length === 0)
      throw new Error("Portal 未绑定 Authentication Provider Catalog");
    for (const service of services) {
      const client = createBrowserPlatformAdminClient(
        context.portal.id,
        service.id,
      );
      const suffix = services.length === 1 ? "" : `/${service.id}`;
      context.addCollectionPage(
        createAuthenticationProviderPage(
          client,
          service.id,
          `/settings/authentication-providers${suffix}`,
          context.i18n.message("page.title", "企业认证 Provider"),
        ),
      );
    }
  },
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": { "page.title": "企业认证 Provider" },
      "en-US": { "page.title": "Enterprise authentication providers" },
    },
  },
};
