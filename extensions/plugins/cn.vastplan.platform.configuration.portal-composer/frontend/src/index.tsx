import { PortalControlClient, type PortalFetch } from "@vastplan/ui-primitives";
import { type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";
import { createPortalPage } from "./portal-workspace";

function createDefaultClient(): PortalControlClient {
  const fetcher: PortalFetch = (input, init) => globalThis.fetch(input, init as RequestInit);
  return new PortalControlClient({ fetch: fetcher });
}

export { buildPortalConfiguration, configurationToForm, portalConfigurationSchema } from "./portal-form";
export { createPortalPage } from "./portal-workspace";

export default {
  register(context: WorkbenchFrontendPluginContext) { context.addCollectionPage(createPortalPage(createDefaultClient())); },
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": { "page.title": "Portal 管理", "page.description": "管理工作副本、发布审批、上线记录和可选版本历史", "page.navigation": "Portal 管理" },
      "en-US": { "page.title": "Portal Management", "page.description": "Manage working copies, publications, releases, and optional version history", "page.navigation": "Portal Management" },
    },
  },
};
