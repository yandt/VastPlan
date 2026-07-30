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
      "zh-CN": { "page.title": "Portal 管理", "page.description": "一个 Portal 管理完整配置、版本历史与上线记录", "page.navigation": "Portal 管理" },
      "en-US": { "page.title": "Portal Management", "page.description": "Manage complete configuration, versions, and releases inside each Portal", "page.navigation": "Portal Management" },
    },
  },
};
