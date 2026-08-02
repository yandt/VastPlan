import type { ComponentSize, PageBodyLayout } from "@vastplan/ui-contract";

type SizeRecipe<T> = Readonly<Record<ComponentSize, Readonly<T>>>;

function sizeRecipe<T>(xs: T, sm: T, md: T, lg: T): SizeRecipe<T> {
  return Object.freeze({ xs: Object.freeze(xs), sm: Object.freeze(sm), md: Object.freeze(md), lg: Object.freeze(lg) });
}

/**
 * 可执行四档组件尺寸。原 sm 几何迁移为 xs，新增 sm 作为紧凑与标准之间的过渡档；
 * Renderers are the only layer allowed to translate these values into framework
 * props and styles.
 */
export const componentSizeRecipes = Object.freeze({
  control: sizeRecipe(
    { height: 24, fontSize: 12, iconEdge: 12, inlinePadding: 8, radius: 2 },
    { height: 28, fontSize: 13, iconEdge: 14, inlinePadding: 10, radius: 3 },
    { height: 32, fontSize: 14, iconEdge: 16, inlinePadding: 12, radius: 4 },
    { height: 40, fontSize: 16, iconEdge: 18, inlinePadding: 16, radius: 6 },
  ),
  iconButton: sizeRecipe(
    { edge: 18, iconEdge: 12, radius: 2 },
    { edge: 24, iconEdge: 12, radius: 3 },
    { edge: 32, iconEdge: 16, radius: 4 },
    { edge: 44, iconEdge: 20, radius: 6 },
  ),
  menu: sizeRecipe(
    { itemHeight: 28, itemInlinePadding: 8, minWidth: 180, surfacePadding: 3, radius: 2 },
    { itemHeight: 32, itemInlinePadding: 10, minWidth: 190, surfacePadding: 3, radius: 3 },
    { itemHeight: 36, itemInlinePadding: 12, minWidth: 200, surfacePadding: 4, radius: 4 },
    { itemHeight: 44, itemInlinePadding: 16, minWidth: 220, surfacePadding: 6, radius: 6 },
  ),
  pagination: sizeRecipe(
    { itemEdge: 24, fontSize: 12 },
    { itemEdge: 28, fontSize: 13 },
    { itemEdge: 32, fontSize: 14 },
    { itemEdge: 40, fontSize: 16 },
  ),
  page: sizeRecipe(
    { padding: 8, headerGap: 8, headerMargin: 8, titleFontSize: 18 },
    { padding: 12, headerGap: 12, headerMargin: 12, titleFontSize: 19 },
    { padding: 24, headerGap: 16, headerMargin: 16, titleFontSize: 20 },
    { padding: 32, headerGap: 24, headerMargin: 24, titleFontSize: 24 },
  ),
  layout: sizeRecipe(
    { gap: 4, flowGap: 8, padding: 8, sectionGap: 8, outerMargin: 0 },
    { gap: 8, flowGap: 12, padding: 12, sectionGap: 12, outerMargin: 0 },
    { gap: 16, flowGap: 16, padding: 16, sectionGap: 24, outerMargin: 0 },
    { gap: 24, flowGap: 24, padding: 24, sectionGap: 32, outerMargin: 0 },
  ),
  descriptions: sizeRecipe(
    { fontSize: 12, titleFontSize: 14, cellPaddingBlock: 4, cellPaddingInline: 8 },
    { fontSize: 13, titleFontSize: 15, cellPaddingBlock: 6, cellPaddingInline: 10 },
    { fontSize: 14, titleFontSize: 16, cellPaddingBlock: 8, cellPaddingInline: 12 },
    { fontSize: 16, titleFontSize: 18, cellPaddingBlock: 12, cellPaddingInline: 16 },
  ),
});

/** Visual treatment is orthogonal to size: a small navigation menu is still navigation. */
export const componentVariantRecipes = Object.freeze({
  menu: Object.freeze({
    action: Object.freeze({ borderInlineEnd: 0, width: "max-content", minWidth: 112, maxWidth: 280, overflow: "hidden", padding: "4px" }),
    actionItem: Object.freeze({ display: "flex", alignItems: "center", width: "100%", gap: "6px", paddingInline: "12px 6px" }),
  }),
});

/** Shell-owned width scale. Pages select semantics and never provide arbitrary CSS or pixels. */
export const pageBodyLayoutRecipes: Readonly<Record<PageBodyLayout, Readonly<{ maxWidth?: number }>>> = Object.freeze({
  fluid: Object.freeze({}),
  large: Object.freeze({ maxWidth: 1280 }),
  medium: Object.freeze({ maxWidth: 960 }),
  small: Object.freeze({ maxWidth: 720 }),
});

/** Combines a page request with an optional Shell-wide cap. Omitted pages default to large. */
export function resolvePageBodyMaxWidth(layout: PageBodyLayout | undefined, shellContained: boolean): number | undefined {
  const pageMaxWidth = pageBodyLayoutRecipes[layout ?? "large"].maxWidth;
  const shellMaxWidth = shellContained ? pageBodyLayoutRecipes.large.maxWidth : undefined;
  if (pageMaxWidth === undefined) return shellMaxWidth;
  if (shellMaxWidth === undefined) return pageMaxWidth;
  return Math.min(pageMaxWidth, shellMaxWidth);
}
