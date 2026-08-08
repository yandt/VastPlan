import { selectDefaultUIProvider } from "@vastplan/plugin-extension-contract";
import { managementServicesFor, message, type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";
import { createNavigationOrganizerClient } from "./management-client.js";
import { createNavigationFolderPage } from "./page.js";

const namespace = "cn.vastplan.product.portal.navigation-organizer";
const providerPoint = `${namespace}.ui-provider`;

export default {
  register(context: WorkbenchFrontendPluginContext) {
    if (selectDefaultUIProvider(context.extensions, providerPoint).kind === "replacement") return;
    const services = managementServicesFor(context.portal, "portal.navigation");
    if (services.length === 0) throw new Error("Portal is not bound to a navigation organizer service");
    const title = context.i18n.message("page.title", "导航编排");
    context.addWorkspacePage({
      id: "portal.navigation-organizer", path: "/settings/navigation", title,
      description: context.i18n.message("page.description", "按服务管理展示文件夹"),
      requiredPermissions: ["portal.navigation.read"],
      navigation: { id: "portal.navigation-organizer", label: title, parentMenuRef: { pluginID: namespace, nodeID: "navigation" }, order: 70 },
      sections: services.map((service) => ({
        id: service.id,
        page: createNavigationFolderPage(
          createNavigationOrganizerClient(context.portal.id, service.id), service.id,
          message(namespace, "section.service", service.label ?? service.id),
        ),
      })),
    });
  },
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": { "navigation.title":"导航编排","page.title":"导航编排","page.description":"按服务管理展示文件夹","section.service":"服务导航","form.id":"文件夹 ID","form.label":"文件夹名称","form.icon":"图标","form.members":"根菜单","form.order":"顺序","form.membersHelp":"每项填写一个插件根菜单 ID，例如 cn.example.plugin/root。至少选择两个根菜单。","form.editTitle":"编辑导航文件夹","form.createTitle":"新建导航文件夹","action.publish":"发布","action.create":"新建文件夹","action.edit":"编辑","action.delete":"删除","notice.published":"导航编排已发布","column.label":"名称","column.icon":"图标","column.count":"菜单数","column.members":"根菜单","column.order":"顺序","error.members":"文件夹至少需要两个根菜单","error.required":"请完整填写文件夹信息","error.duplicate":"文件夹 ID 已存在","confirm.delete":"删除此文件夹并发布新的 Portal Generation？" },
    "en-US": { "navigation.title":"Navigation organizer","page.title":"Navigation organizer","page.description":"Manage presentation folders by service","section.service":"Service navigation","form.id":"Folder ID","form.label":"Folder name","form.icon":"Icon","form.members":"Root menus","form.order":"Order","form.membersHelp":"Enter one plugin root menu ID per item, for example cn.example.plugin/root. At least two roots are required.","form.editTitle":"Edit navigation folder","form.createTitle":"Create navigation folder","action.publish":"Publish","action.create":"Create folder","action.edit":"Edit","action.delete":"Delete","notice.published":"Navigation organization published","column.label":"Name","column.icon":"Icon","column.count":"Menus","column.members":"Root menus","column.order":"Order","error.members":"A folder requires at least two root menus","error.required":"Complete the folder fields","error.duplicate":"The folder ID already exists","confirm.delete":"Delete this folder and publish a new Portal Generation?" }
  } },
};

export { createNavigationFolderPage } from "./page.js";
export { NavigationOrganizerClient } from "./management-client.js";
