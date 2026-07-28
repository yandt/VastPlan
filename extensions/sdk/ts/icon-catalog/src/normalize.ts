import type { IconGlyphDefinition, IconGlyphNode, IconGroupNode, IconPathNode } from "./types.js";

interface AntNode { readonly tag: string; readonly attrs?: Readonly<Record<string, string>>; readonly children?: readonly AntNode[]; }
interface AntIconDefinition { readonly name: string; readonly theme: string; readonly icon: AntNode | ((primary: string, secondary: string) => AntNode); }
interface AntIconModule { readonly default: AntIconDefinition; }

const primaryMarker = "__VASTPLAN_PRIMARY__";
const secondaryMarker = "__VASTPLAN_SECONDARY__";

/** Converts the locked MIT upstream definition into a small CSP-safe render tree. */
export function normalizeAntIcon(input: AntIconDefinition | AntIconModule): IconGlyphDefinition {
  const definition = "icon" in input ? input : input.default;
  const root = typeof definition.icon === "function" ? definition.icon(primaryMarker, secondaryMarker) : definition.icon;
  if (root.tag !== "svg" || !validViewBox(root.attrs?.viewBox)) throw new Error(`Ant 图标 viewBox 无效: ${definition.name}`);
  const nodes = normalizeChildren(root.children ?? []);
  if (countPaths(nodes) === 0) throw new Error(`Ant 图标没有可渲染路径: ${definition.name}`);
  const fillRule = normalizeFillRule(root.attrs?.["fill-rule"]);
  return Object.freeze({ viewBox: root.attrs!.viewBox!, ...(fillRule === undefined ? {} : { fillRule }), nodes: Object.freeze(nodes) });
}

function normalizeChildren(children: readonly AntNode[]): IconGlyphNode[] {
  const result = [];
  for (const child of children) {
    if (child.tag === "path") result.push(normalizePath(child));
    else if (child.tag === "g") result.push(normalizeGroup(child));
    else if (child.tag !== "defs" && child.tag !== "style" && child.tag !== "filter") throw new Error(`Ant 图标包含未知 SVG 节点: ${child.tag}`);
  }
  return result;
}

function normalizePath(node: AntNode): IconPathNode {
  const d = node.attrs?.d;
  if (typeof d !== "string" || d.length === 0 || d.length > 32_768 || !/^[MmZzLlHhVvCcSsQqTtAa0-9.,+eE \-]+$/.test(d)) throw new Error("Ant 图标 path 无效");
  const fill = node.attrs?.fill;
  if (fill !== undefined && fill !== primaryMarker && fill !== secondaryMarker) throw new Error("Ant 图标 fill 不在白名单");
  const sourceOpacity = node.attrs?.["fill-opacity"] === undefined ? 1 : Number(node.attrs["fill-opacity"]);
  if (!Number.isFinite(sourceOpacity) || sourceOpacity < 0 || sourceOpacity > 1) throw new Error("Ant 图标 opacity 无效");
  const tone = fill === secondaryMarker ? "secondary" : "primary";
  const opacity = sourceOpacity * (tone === "secondary" ? 0.35 : 1);
  const fillRule = normalizeFillRule(node.attrs?.["fill-rule"]);
  return Object.freeze({ tag: "path", d, tone, ...(opacity === 1 ? {} : { opacity }), ...(fillRule === undefined ? {} : { fillRule }) });
}

function normalizeGroup(node: AntNode): IconGroupNode {
  const transform = node.attrs?.transform;
  if (transform !== undefined && (transform.length > 128 || !/^[A-Za-z0-9.,() +\-]+$/.test(transform))) throw new Error("Ant 图标 transform 无效");
  return Object.freeze({ tag: "g", ...(transform === undefined ? {} : { transform }), children: Object.freeze(normalizeChildren(node.children ?? [])) });
}

function validViewBox(value: string | undefined): value is string {
  if (value === undefined) return false;
  const parts = value.trim().split(/\s+/).map(Number);
  return parts.length === 4 && parts.every(Number.isFinite) && parts[2] > 0 && parts[2] <= 4096 && parts[3] > 0 && parts[3] <= 4096;
}

function normalizeFillRule(value: string | undefined): "evenodd" | "nonzero" | undefined {
  if (value === undefined) return undefined;
  if (value !== "evenodd" && value !== "nonzero") throw new Error("Ant 图标 fill-rule 无效");
  return value;
}

function countPaths(nodes: readonly IconGlyphNode[]): number {
  return nodes.reduce((total, node) => total + (node.tag === "path" ? 1 : countPaths(node.children)), 0);
}
