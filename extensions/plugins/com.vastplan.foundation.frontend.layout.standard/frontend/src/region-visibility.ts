import type { NavigationZone, ShellCompositionModel, ShellSlotID } from "@vastplan/portal-ui";

export interface RegionContent {
  intrinsic?: boolean;
  navigationZones?: readonly NavigationZone[];
  slots?: readonly ShellSlotID[];
}

/** A layout region exists only when the composition or layout puts real content in it. */
export function hasRegionContent(composition: ShellCompositionModel, content: RegionContent): boolean {
  if (content.intrinsic === true) return true;
  if (content.navigationZones?.some((zone) => composition.navigation[zone].length > 0) === true) return true;
  return content.slots?.some((id) => (composition.slots[id]?.length ?? 0) > 0) === true;
}
