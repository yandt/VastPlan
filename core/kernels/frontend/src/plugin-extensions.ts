import { emptyPortalExtensionGraph, parsePortalExtensionGraph } from "@vastplan/plugin-extension-contract";
import type { PluginExtensionAccess, PortalExtensionGraph } from "@vastplan/plugin-extension-contract";
import type { PortalRegisteredPage } from "@vastplan/ui-primitives";
import { PortalAssemblyError } from "./portal-errors";
import type { PluginRef } from "./portal-contracts";

export { emptyPortalExtensionGraph, parsePortalExtensionGraph };

export function validateExtensionGraphForPortal(graph: PortalExtensionGraph, plugins: readonly PluginRef[]): void {
  const pluginIDs = new Set(plugins.map((plugin) => plugin.id));
  for (const point of graph.points) {
    if (point.surface !== "frontend") throw new PortalAssemblyError("EXTENSION_SURFACE_INVALID", `Portal 扩展点 surface 非 frontend: ${point.id}`);
    if (!pluginIDs.has(point.ownerPluginId)) throw new PortalAssemblyError("EXTENSION_OWNER_MISSING", `扩展点所有者未装配: ${point.id}`);
  }
  for (const contribution of graph.contributions) {
    if (!pluginIDs.has(contribution.pluginId)) throw new PortalAssemblyError("EXTENSION_CONTRIBUTOR_MISSING", `扩展贡献插件未装配: ${contribution.id}`);
  }
}

export function createPluginExtensionAccess(graph: PortalExtensionGraph, pluginID: string): PluginExtensionAccess {
  const owned = new Set(graph.points.filter((point) => point.ownerPluginId === pluginID).map((point) => point.id));
  const visible = graph.contributions.filter((contribution) => owned.has(contribution.point) || contribution.pluginId === pluginID);
  return Object.freeze({
    owns: (pointID: string) => owned.has(pointID),
    contributes: (pointID: string) => visible.some((contribution) => contribution.point === pointID && contribution.pluginId === pluginID),
    list: (pointID: string) => Object.freeze(visible.filter((contribution) => contribution.point === pointID)),
  });
}

/** Enforces the first governed kind: pages mounted into another plugin's navigation targets. */
export function validateFrontendPageExtensions(pages: readonly PortalRegisteredPage[], graph: PortalExtensionGraph): void {
  const points = graph.points.filter((point) => point.kind === "frontend.page");
  const pointByID = new Map(points.map((point) => [point.id, point]));
  const pageByID = new Map(pages.map((page) => [page.id, page]));
  const authorized = new Set<string>();

  for (const contribution of graph.contributions) {
    const point = pointByID.get(contribution.point);
    if (point === undefined) continue;
    const pageID = stringField(contribution.descriptor, "pageId");
    const groupID = stringField(contribution.descriptor, "groupId");
    const page = pageByID.get(pageID);
    if (page === undefined || page.pluginID !== contribution.pluginId || page.navigation?.parentMenuRef.pluginID !== point.ownerPluginId || page.navigation.parentMenuRef.nodeID !== groupID || !(point.targets ?? []).includes(groupID)) {
      throw new PortalAssemblyError("EXTENSION_PAGE_MISMATCH", `扩展页面未按签名 descriptor 注册: ${contribution.id}`);
    }
    authorized.add(`${contribution.pluginId}\u0000${pageID}\u0000${groupID}`);
  }

  for (const page of pages) {
    const groupID = page.navigation?.parentMenuRef.nodeID;
    if (groupID === undefined) continue;
    const point = points.find((candidate) => (candidate.targets ?? []).includes(groupID));
    if (point === undefined || page.pluginID === point.ownerPluginId) continue;
    if (!authorized.has(`${page.pluginID}\u0000${page.id}\u0000${groupID}`)) {
      throw new PortalAssemblyError("EXTENSION_PAGE_UNAUTHORIZED", `插件未声明扩展关系却挂载受治理页面: ${page.pluginID}/${page.id}`);
    }
  }
}

function stringField(value: Readonly<Record<string, unknown>>, key: string): string {
  const field = value[key];
  if (typeof field !== "string" || field.length === 0) throw new PortalAssemblyError("EXTENSION_PAGE_DESCRIPTOR_INVALID", `frontend.page descriptor 缺少 ${key}`);
  return field;
}
