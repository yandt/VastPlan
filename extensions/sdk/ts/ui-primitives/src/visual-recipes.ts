/**
 * Framework-neutral pixel recipe for dense record actions.
 *
 * Workbench chooses the semantic `compact` density. Renderers are the only
 * layer allowed to translate these governed values into framework styles.
 */
export const compactActionVisualRecipe = Object.freeze({
  control: Object.freeze({
    edge: 18,
    iconEdge: 12,
    radius: 2,
  }),
  menu: Object.freeze({
    itemHeight: 28,
    itemInlinePadding: 8,
    minWidth: 180,
    surfacePadding: 3,
    radius: 2,
    borderInlineEnd: 0,
  }),
});
