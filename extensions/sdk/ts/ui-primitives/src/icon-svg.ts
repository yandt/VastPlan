import { createElement } from "react";
import type { CSSProperties, ReactElement, ReactNode } from "react";
import type { IconGlyphDefinition, IconGlyphNode } from "@vastplan/icon-catalog/semantic";

const pixels = { sm: 16, md: 20, lg: 24 } as const;

interface IconGlyphRenderOptions {
  readonly name: string;
  readonly label?: string;
  readonly size: "sm" | "md" | "lg";
  readonly className?: string;
  readonly style?: CSSProperties;
}

export function renderIconGlyph(glyph: IconGlyphDefinition, options: IconGlyphRenderOptions): ReactElement {
  return createElement("svg", {
    "data-vastplan-icon": options.name,
    "data-vastplan-icon-source": "canonical",
    viewBox: glyph.viewBox,
    width: pixels[options.size],
    height: pixels[options.size],
    fill: "currentColor",
    fillRule: glyph.fillRule,
    stroke: "none",
    className: options.className,
    style: { display: "inline-block", flex: "0 0 auto", verticalAlign: "middle", ...options.style },
    focusable: "false",
    role: options.label === undefined ? undefined : "img",
    "aria-label": options.label,
    "aria-hidden": options.label === undefined ? true : undefined,
  }, glyph.nodes.map((node, index) => renderNode(node, `n${index}`)));
}

function renderNode(node: IconGlyphNode, key: string): ReactNode {
  if (node.tag === "path") {
    return createElement("path", { key, d: node.d, opacity: node.opacity, fillRule: node.fillRule });
  }
  return createElement("g", { key, transform: node.transform }, node.children.map((child, index) => renderNode(child, `${key}-${index}`)));
}
