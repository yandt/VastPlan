import { contributionsByKind, type ContributionIndexSnapshot } from "@vastplan/plugin-inventory-contract";
import { message, semanticIconNames, type PortalNavigationCatalog, type PortalNavigationCatalogNode, type PortalNavigationIconSpec, type PortalNavigationNodeRef } from "@vastplan/ui-primitives";
import type { IconGlyphDefinition, IconGlyphNode } from "@vastplan/icon-catalog/semantic";
import { PortalAssemblyError } from "./portal-errors";

const localIDPattern = /^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$/;
const pluginIDPattern = /^[a-z0-9]+(?:[.-][a-z0-9]+)+$/;
const pathPattern = /^[MmZzLlHhVvCcSsQqTtAa0-9.,+eE \-]+$/;
const transformPattern = /^(?:(?:matrix|translate|scale|rotate|skewX|skewY)\([0-9eE.,+\- ]+\)\s*)+$/;
const semanticIcons = new Set<string>(semanticIconNames);
const zones = new Set(["primary", "secondary", "settings"]);
const motions = new Set(["none", "pulse", "spin", "draw"]);
const states = new Set(["normal", "active", "loading", "error"]);

/** Parses code-free signed navigation contributions before any plugin registers pages. */
export function navigationCatalogsFromIndex(index: ContributionIndexSnapshot | undefined): readonly PortalNavigationCatalog[] {
  if (index === undefined) return Object.freeze([]);
  const contributions = contributionsByKind(index, "frontend.navigations");
  if (contributions.length > 512) throw invalid("Portal 导航贡献超过 512 个");
  let totalBytes = 0;
  let totalNodes = 0;
  const catalogs = contributions.map((contribution) => {
    const descriptor = contribution.descriptor;
    totalBytes += new TextEncoder().encode(JSON.stringify(descriptor)).byteLength;
    if (totalBytes > 1 << 20) throw invalid("Portal 导航贡献超过 1 MiB");
    if (contribution.id !== "main" || contribution.contract !== "1.0.0" || descriptor.id !== "main" || descriptor.contract !== "1.0.0" || !Array.isArray(descriptor.nodes) || descriptor.nodes.length === 0 || descriptor.nodes.length > 64 || !Array.isArray(descriptor.icons) || descriptor.icons.length > 16) {
      throw invalid(`导航目录身份或结构无效: ${contribution.owner.ref.pluginId}`);
    }
    totalNodes += descriptor.nodes.length;
    if (totalNodes > 512) throw invalid("Portal 导航节点超过 512 个");
    const icons = parseIcons(contribution.owner.ref.pluginId, descriptor.icons);
    const nodeIDs = new Set<string>();
    const nodes = descriptor.nodes.map((value) => parseNode(contribution.owner.ref.pluginId, value, icons, nodeIDs));
    return Object.freeze({ pluginID: contribution.owner.ref.pluginId, nodes: Object.freeze(nodes) });
  });
  assertCatalogGraph(catalogs);
  return Object.freeze(catalogs);
}

function parseIcons(pluginID: string, values: readonly unknown[]): ReadonlyMap<string, PortalNavigationIconSpec & { kind: "custom" }> {
  const icons = new Map<string, PortalNavigationIconSpec & { kind: "custom" }>();
  let bytes = 0;
  for (const value of values) {
    if (!record(value) || !localID(value.id) || icons.has(value.id) || !record(value.states) || !motions.has(String(value.motion))) throw invalid(`自定义导航图标无效: ${pluginID}`);
    bytes += new TextEncoder().encode(JSON.stringify(value)).byteLength;
    if (bytes > 128 << 10) throw invalid(`插件导航图标目录超过 128 KiB: ${pluginID}`);
    const parsedStates: Partial<Record<"normal" | "active" | "loading" | "error", IconGlyphDefinition>> = {};
    for (const [state, glyph] of Object.entries(value.states)) {
      if (!states.has(state)) throw invalid(`导航图标状态无效: ${pluginID}/${value.id}/${state}`);
      parsedStates[state as keyof typeof parsedStates] = parseGlyph(glyph, 0);
    }
    if (parsedStates.normal === undefined) throw invalid(`导航图标缺少 normal 状态: ${pluginID}/${value.id}`);
    const frozenStates = Object.freeze({ ...parsedStates, normal: parsedStates.normal });
    icons.set(value.id, Object.freeze({ kind: "custom", pluginID, name: value.id, states: frozenStates, motion: String(value.motion) as "none" | "pulse" | "spin" | "draw" }));
  }
  return icons;
}

function parseNode(pluginID: string, value: unknown, icons: ReadonlyMap<string, PortalNavigationIconSpec & { kind: "custom" }>, seen: Set<string>): PortalNavigationCatalogNode {
  if (!record(value) || !localID(value.id) || seen.has(value.id) || !zones.has(String(value.zone)) || !record(value.label) || !localID(value.label.key) || typeof value.label.fallback !== "string" || value.label.fallback.trim() === "" || value.label.fallback.length > 80 || !record(value.icon) || !Number.isSafeInteger(value.order ?? 0) || Math.abs(Number(value.order ?? 0)) > 1_000_000) {
    throw invalid(`导航节点无效: ${pluginID}`);
  }
  seen.add(value.id);
  let icon: PortalNavigationIconSpec;
  if (value.icon.kind === "semantic" && typeof value.icon.name === "string" && semanticIcons.has(value.icon.name)) icon = Object.freeze({ kind: "semantic", name: value.icon.name as never });
  else if (value.icon.kind === "custom" && typeof value.icon.name === "string" && icons.has(value.icon.name)) icon = icons.get(value.icon.name)!;
  else throw invalid(`导航节点图标无效: ${pluginID}/${value.id}`);
  const ref = Object.freeze({ pluginID, nodeID: value.id });
  return Object.freeze({ id: navigationNodeKey(ref), ref, label: message(pluginID, value.label.key, value.label.fallback.trim()), zone: value.zone as "primary" | "secondary" | "settings", icon, ...(value.parent === undefined ? {} : { parent: parseParent(pluginID, value.parent) }), ...(value.order === undefined ? {} : { order: Number(value.order) }) });
}

function parseParent(pluginID: string, value: unknown): PortalNavigationCatalogNode["parent"] {
  if (!record(value) || !localID(value.nodeId) || !["required", "optional"].includes(String(value.mode)) || (value.pluginId !== undefined && (typeof value.pluginId !== "string" || value.pluginId.length > 160 || !pluginIDPattern.test(value.pluginId)))) throw invalid(`导航父级引用无效: ${pluginID}`);
  const owner = typeof value.pluginId === "string" ? value.pluginId : pluginID;
  if (value.mode === "optional") {
    if (owner === pluginID || !localID(value.fallbackNodeId)) throw invalid(`optional 导航父级必须跨插件并声明自有回退: ${pluginID}`);
    return Object.freeze({ pluginID: owner, nodeID: value.nodeId, mode: "optional", fallback: Object.freeze({ pluginID, nodeID: value.fallbackNodeId }) });
  }
  if (value.fallbackNodeId !== undefined) throw invalid(`required 导航父级不能声明回退: ${pluginID}`);
  return Object.freeze({ pluginID: owner, nodeID: value.nodeId, mode: "required" });
}

function assertCatalogGraph(catalogs: readonly PortalNavigationCatalog[]): void {
  const nodes = new Map(catalogs.flatMap((catalog) => catalog.nodes.map((node) => [node.id, node] as const)));
  if (nodes.size !== catalogs.reduce((count, catalog) => count + catalog.nodes.length, 0)) throw invalid("Portal 导航节点全局身份重复");
  const resolvedParent = (node: PortalNavigationCatalogNode): PortalNavigationCatalogNode | undefined => {
    if (node.parent === undefined) return undefined;
    const target = nodes.get(navigationNodeKey(node.parent));
    if (target !== undefined) return target;
    if (node.parent.mode === "optional" && node.parent.fallback !== undefined) return nodes.get(navigationNodeKey(node.parent.fallback));
    throw invalid(`Portal 导航引用未知 required 父级: ${node.id}`);
  };
  for (const node of nodes.values()) {
    const parent = resolvedParent(node);
    if (parent === undefined) continue;
    if (parent.id === node.id || resolvedParent(parent) !== undefined) throw invalid(`Portal 导航循环或深度超过一级菜单、二级菜单、页面: ${node.id}`);
    if (parent.zone !== node.zone) throw invalid(`Portal 导航不能跨 zone: ${node.id}/${parent.id}`);
  }
}

function parseGlyph(value: unknown, depth: number): IconGlyphDefinition {
  if (depth > 4 || !record(value) || typeof value.viewBox !== "string" || !validViewBox(value.viewBox) || !Array.isArray(value.nodes) || value.nodes.length === 0 || value.nodes.length > 32 || (value.fillRule !== undefined && value.fillRule !== "evenodd" && value.fillRule !== "nonzero")) throw invalid("导航 SVG 图元无效");
  const nodes = value.nodes.map((node) => parseGlyphNode(node, depth + 1));
  return Object.freeze({ viewBox: value.viewBox, ...(value.fillRule === undefined ? {} : { fillRule: value.fillRule }), nodes: Object.freeze(nodes) });
}

function parseGlyphNode(value: unknown, depth: number): IconGlyphNode {
  if (depth > 4 || !record(value)) throw invalid("导航 SVG 节点深度无效");
  if (value.tag === "path" && typeof value.d === "string" && value.d.length > 0 && value.d.length <= 32768 && pathPattern.test(value.d) && (value.tone === "primary" || value.tone === "secondary") && (value.opacity === undefined || (typeof value.opacity === "number" && Number.isFinite(value.opacity) && value.opacity >= 0 && value.opacity <= 1)) && (value.fillRule === undefined || value.fillRule === "evenodd" || value.fillRule === "nonzero")) return Object.freeze({ tag: "path", d: value.d, tone: value.tone, ...(value.opacity === undefined ? {} : { opacity: value.opacity }), ...(value.fillRule === undefined ? {} : { fillRule: value.fillRule }) });
  if (value.tag === "g" && (value.transform === undefined || (typeof value.transform === "string" && value.transform.length <= 128 && transformPattern.test(value.transform))) && Array.isArray(value.children) && value.children.length > 0 && value.children.length <= 32) return Object.freeze({ tag: "g", ...(value.transform === undefined ? {} : { transform: value.transform }), children: Object.freeze(value.children.map((child) => parseGlyphNode(child, depth + 1))) });
  throw invalid("导航 SVG 只允许安全 path/group 节点");
}

function validViewBox(value: string): boolean { const parts = value.trim().split(/\s+/).map(Number); return parts.length === 4 && parts.every(Number.isFinite) && parts[2] > 0 && parts[2] <= 4096 && parts[3] > 0 && parts[3] <= 4096; }
function navigationNodeKey(ref: PortalNavigationNodeRef): string { return `${ref.pluginID}/${ref.nodeID}`; }
function localID(value: unknown): value is string { return typeof value === "string" && value.length <= 160 && localIDPattern.test(value); }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function invalid(message: string): PortalAssemblyError { return new PortalAssemblyError("NAVIGATION_CONTRIBUTION_INVALID", message); }
