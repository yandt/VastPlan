import type { ComponentSize } from "@vastplan/ui-contract";

type SizeRecipe<T> = Readonly<Record<ComponentSize, Readonly<T>>>;

function sizeRecipe<T>(sm: T, md: T, lg: T): SizeRecipe<T> {
  return Object.freeze({ sm: Object.freeze(sm), md: Object.freeze(md), lg: Object.freeze(lg) });
}

/**
 * Executable three-step component scale. Workbench selects semantic density;
 * Renderers are the only layer allowed to translate these values into framework
 * props and styles.
 */
export const componentSizeRecipes = Object.freeze({
  control: sizeRecipe(
    { height: 24, fontSize: 12, iconEdge: 12, inlinePadding: 8, radius: 2 },
    { height: 32, fontSize: 14, iconEdge: 16, inlinePadding: 12, radius: 4 },
    { height: 40, fontSize: 16, iconEdge: 18, inlinePadding: 16, radius: 6 },
  ),
  iconButton: sizeRecipe(
    { edge: 18, iconEdge: 12, radius: 2 },
    { edge: 32, iconEdge: 16, radius: 4 },
    { edge: 44, iconEdge: 20, radius: 6 },
  ),
  menu: sizeRecipe(
    { itemHeight: 28, itemInlinePadding: 8, minWidth: 180, surfacePadding: 3, radius: 2 },
    { itemHeight: 36, itemInlinePadding: 12, minWidth: 200, surfacePadding: 4, radius: 4 },
    { itemHeight: 44, itemInlinePadding: 16, minWidth: 220, surfacePadding: 6, radius: 6 },
  ),
  pagination: sizeRecipe(
    { itemEdge: 24, fontSize: 12 },
    { itemEdge: 32, fontSize: 14 },
    { itemEdge: 40, fontSize: 16 },
  ),
});

/** Visual treatment is orthogonal to size: a small navigation menu is still navigation. */
export const componentVariantRecipes = Object.freeze({
  menu: Object.freeze({
    action: Object.freeze({ borderInlineEnd: 0, width: "max-content", minWidth: 112, maxWidth: 280, overflow: "hidden" }),
    actionItem: Object.freeze({ display: "flex", alignItems: "center", gap: "8px" }),
  }),
});
