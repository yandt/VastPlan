import type { PageSlotID, StructureCompositionModel, ShellSlotID } from "@vastplan/ui-primitives";

export interface RegionContent {
  intrinsic?: boolean;
  navigationGroups?: boolean;
  shellSlots?: readonly ShellSlotID[];
  pageSlots?: readonly PageSlotID[];
}

/** A layout region exists only when the composition or layout puts real content in it. */
export function hasRegionContent(composition: StructureCompositionModel, content: RegionContent): boolean {
  if (content.intrinsic === true) return true;
  if (content.navigationGroups === true && Object.values(composition.navigation).some((groups) => groups.length > 0)) return true;
  if (content.shellSlots?.some((slot) => (composition.shellSlots[slot]?.length ?? 0) > 0) === true) return true;
  return content.pageSlots?.some((slot) => (composition.pageSlots[slot]?.length ?? 0) > 0) === true;
}
