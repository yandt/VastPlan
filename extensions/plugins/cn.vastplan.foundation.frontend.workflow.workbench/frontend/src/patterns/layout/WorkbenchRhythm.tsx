import type { CSSProperties, ReactNode } from "react";
import { portalPageRhythm, type PortalPageDensity, type WorkbenchComponentInset } from "@vastplan/ui-primitives";

const baseRootStyle: CSSProperties = {
  boxSizing: "border-box",
  width: "100%",
  minWidth: 0,
  margin: 0,
  padding: 0,
};

/** Owns spacing between page-level Workbench regions; children must not add outer margins. */
export function WorkbenchPageFlow({ density = "standard", children }: {
  density?: PortalPageDensity;
  children: ReactNode;
}) {
  return <div data-vp-workbench-flow={density} style={workbenchPageFlowStyle(density)}>{children}</div>;
}

export function workbenchPageFlowStyle(density: PortalPageDensity): CSSProperties {
  return {
    ...baseRootStyle,
    display: "flex",
    flexDirection: "column",
    alignItems: "stretch",
    gap: portalPageRhythm.sectionGap[density],
  };
}

/** Gives a composed component an explicit inset without changing its page position. */
export function WorkbenchComponentRegion({ inset = "flush", children }: {
  inset?: WorkbenchComponentInset;
  children: ReactNode;
}) {
  return <div data-vp-workbench-inset={inset} style={workbenchComponentRegionStyle(inset)}>{children}</div>;
}

export function workbenchComponentRegionStyle(inset: WorkbenchComponentInset): CSSProperties {
  return {
    ...baseRootStyle,
    padding: portalPageRhythm.componentInset[inset],
  };
}
