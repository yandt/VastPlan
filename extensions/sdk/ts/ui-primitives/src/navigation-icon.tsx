import type { ReactElement } from "react";
import type { ComponentSize } from "@vastplan/ui-contract";
import { VastPlanIcon } from "./icon.js";
import { renderIconGlyph } from "./icon-svg.js";
import type { PortalNavigationIconState, PortalNavigationPresentationIcon } from "./portal-runtime.js";

export function NavigationIcon({ icon, state = "normal", label, size = "md" }: {
  icon: PortalNavigationPresentationIcon;
  state?: PortalNavigationIconState;
  label?: string;
  size?: ComponentSize;
}): ReactElement {
  if (icon.kind === "composite") return <span
    data-vastplan-navigation-composite
    role={label === undefined ? undefined : "img"}
    aria-label={label}
    style={{ display: "inline-grid", gridTemplateColumns: "repeat(2,minmax(0,1fr))", gridTemplateRows: "repeat(2,minmax(0,1fr))", width: "1.35em", height: "1.35em", gap: "0.08em", placeItems: "center", overflow: "hidden" }}
  >{icon.items.slice(0, 4).map((item, index) => <span key={`${item.kind}:${index}`} aria-hidden="true" style={{ display: "grid", placeItems: "center", width: "100%", height: "100%", fontSize: "0.46em", lineHeight: 1 }}><NavigationIcon icon={item} state={state} size={size} /></span>)}</span>;
  if (icon.kind === "semantic") return <VastPlanIcon name={icon.name} label={label} size={size} />;
  const resolvedState = icon.states[state] ?? icon.states.normal;
  return <span data-vastplan-navigation-icon data-motion={state === "active" || state === "loading" ? icon.motion : "none"}>
    {renderIconGlyph(resolvedState, { name: `${icon.pluginID}/${icon.name}`, label, size })}
  </span>;
}
