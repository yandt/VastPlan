import { createBrowserPlatformAdminClient } from "@vastplan/platform-admin";
import { managementServicesFor, type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";
import { auditPage } from "./pages/audit.js";
import { bindingsPage } from "./pages/bindings.js";
import { permissionsPage } from "./pages/permissions.js";
import { rolesPage } from "./pages/roles.js";

export default {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.authorization");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.authorization 服务");
    for (const service of services) {
      const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
      const suffix = services.length === 1 ? "" : `.${service.id}`;
      const pathSuffix = services.length === 1 ? "" : `/${service.id}`;
      context.addCollectionPage(permissionsPage(client, service.id, suffix, pathSuffix));
      context.addCollectionPage(rolesPage(client, service.id, suffix, pathSuffix));
      context.addCollectionPage(bindingsPage(client, service.id, suffix, pathSuffix));
      context.addCollectionPage(auditPage(client, service.id, suffix, pathSuffix));
    }
  },
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": { "navigation.security": "安全与授权", "permissions.title": "权限目录", "roles.title": "角色管理", "bindings.title": "主体绑定", "audit.title": "授权审计", "action.createRole": "新建角色", "action.createBinding": "新建绑定", "action.publishSnapshot": "发布策略快照" },
      "en-US": { "navigation.security": "Security and authorization", "permissions.title": "Permission catalog", "roles.title": "Roles", "bindings.title": "Subject bindings", "audit.title": "Authorization audit", "action.createRole": "New role", "action.createBinding": "New binding", "action.publishSnapshot": "Publish policy snapshot" },
    },
  },
};

export { permissionsPage, rolesPage, bindingsPage, auditPage };
