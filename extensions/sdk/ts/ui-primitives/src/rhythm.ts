/**
 * Cross-renderer page rhythm owned by the Portal design system.
 *
 * Shell owns contentStart; Workbench owns sectionGap and component inset.
 * Functional plugins never compensate these values with margins.
 */
export const portalPageRhythm = Object.freeze({
  contentStart: 16,
  sectionGap: Object.freeze({ compact: 8, standard: 16, comfortable: 24 }),
  componentInset: Object.freeze({ flush: 0, compact: 8 }),
});

export type PortalPageDensity = keyof typeof portalPageRhythm.sectionGap;
export type WorkbenchComponentInset = keyof typeof portalPageRhythm.componentInset;
