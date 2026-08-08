import { selectDefaultUIProvider } from "@vastplan/plugin-extension-contract";
import { managementServicesFor, type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";
import { createWorkflowManagementClient } from "./management-client.js";
import { createWorkflowPages } from "./pages.js";

const namespace = "cn.vastplan.platform.workflow.orchestrator";
const providerPoint = `${namespace}.ui-provider`;

export default {
  register(context: WorkbenchFrontendPluginContext) {
    if (selectDefaultUIProvider(context.extensions, providerPoint).kind === "replacement") return;
    const services = managementServicesFor(context.portal, "platform.workflow.orchestrator");
    if (services.length === 0) throw new Error("Portal is not bound to a workflow orchestrator service");
    for (const service of services) {
      const suffix = services.length === 1 ? "" : `/${encodeURIComponent(service.id)}`;
      const label = service.label ?? service.id;
      for (const page of createWorkflowPages(createWorkflowManagementClient(context.portal.id, service.id), service.id, suffix, label)) context.addCollectionPage(page);
    }
  },
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": { "navigation.title":"流程管理","catalog.navigation":"流程目录","definitions.navigation":"流程定义","bindings.navigation":"服务绑定","instances.navigation":"流程实例","tasks.navigation":"待办任务" },
    "en-US": { "navigation.title":"Workflows","catalog.navigation":"Catalog","definitions.navigation":"Definitions","bindings.navigation":"Bindings","instances.navigation":"Instances","tasks.navigation":"Tasks" }
  } },
};

export { createWorkflowPages } from "./pages.js";
export { WorkflowManagementClient, createWorkflowManagementClient } from "./management-client.js";
