/** Workbench 与 Renderer 共享的四档组件尺寸。 */
export const componentSizes = Object.freeze(["xs", "sm", "md", "lg"] as const);
export type ComponentSize = (typeof componentSizes)[number];
/** Overlay 几何宽度不使用 xs；xs 只表达内容密度。 */
export const overlayWidths = Object.freeze(["sm", "md", "lg"] as const);
export type OverlayWidth = (typeof overlayWidths)[number];

/** 仅承载组件几何尺寸；宽度、布局、密度和用途必须使用各自独立属性。 */
export interface SizeableProps {
  size?: ComponentSize;
}
