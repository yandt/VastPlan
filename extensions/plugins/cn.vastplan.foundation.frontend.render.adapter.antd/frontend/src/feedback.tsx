import { Alert, Button as AntdButton, Drawer as AntdDrawer, Empty, Modal, Skeleton as AntdSkeleton, Spin, Tooltip as AntdTooltip, Typography } from "antd";
import type { ComponentType, CSSProperties } from "react";
import type { BusyProps, ButtonProps, DialogProps, DrawerProps, EmptyStateProps, ErrorStateProps, IconButtonProps, SkeletonProps, TooltipProps, VastPlanIconProps } from "@vastplan/ui-primitives";
import { ComponentSizeProvider, componentSizeRecipes, dialogBodyStyle, dialogFrameStyle, message, useComponentSize, usePortalI18n, VastPlanIcon } from "@vastplan/ui-primitives";
import { antdComponentSize } from "./component-size";
import { dialogWidths, namespace } from "./theme";

function buttonStyle(kind: ButtonProps["kind"]): { type?: "primary" | "default" | "text"; danger?: boolean } {
  if (kind === "primary") return { type: "primary" };
  if (kind === "danger") return { type: "default", danger: true };
  if (kind === "text") return { type: "text" };
  return { type: "default" };
}

export function Button({ children, kind, size: requestedSize, ...props }: ButtonProps) {
  const size = useComponentSize(requestedSize);
  const recipe = componentSizeRecipes.control[size];
  return <AntdButton {...buttonStyle(kind)} size={antdComponentSize[size]} style={{ height: recipe.height, paddingInline: recipe.inlinePadding, borderRadius: recipe.radius, fontSize: recipe.fontSize }} {...props}>{children}</AntdButton>;
}

const tooltipPlacements = {
  "top-start": "topLeft",
  top: "top",
  "top-end": "topRight",
  "bottom-start": "bottomLeft",
  bottom: "bottom",
  "bottom-end": "bottomRight",
} as const;

/** Maps the governed Tooltip contract onto Ant Design without leaking Ant props to Shell code. */
export function Tooltip({ title, children, placement = "top" }: TooltipProps) {
  return <AntdTooltip title={title} placement={tooltipPlacements[placement]}>{children}</AntdTooltip>;
}

export function iconButtonWith(Icon: ComponentType<VastPlanIconProps>, { icon, label, size: requestedSize, onClick, disabled, loading, tone = "normal" }: IconButtonProps) {
  const size = useComponentSize(requestedSize);
  const recipe = componentSizeRecipes.iconButton[size];
  return <AntdTooltip title={label}><AntdButton
    aria-label={label}
    type={tone === "primary" ? "primary" : "text"}
    danger={tone === "danger"}
    size={antdComponentSize[size]}
    icon={<Icon name={icon} size={size} style={{ width: recipe.iconEdge, height: recipe.iconEdge }} />}
    loading={loading}
    disabled={disabled}
    onClick={onClick}
    style={{ width: recipe.edge, height: recipe.edge, borderRadius: recipe.radius }}
  /></AntdTooltip>;
}

export function IconButton(props: IconButtonProps) { return iconButtonWith(VastPlanIcon, props); }

export function Dialog({ open, title, description, children, footer, variant = "default", width = "md", size: requestedSize, height, contentOverflow = "scroll", onClose }: DialogProps) {
  const scrollable = contentOverflow === "scroll";
  const size = useComponentSize(requestedSize);
  const recipe = componentSizeRecipes.layout[size];
  const formRecipe = componentSizeRecipes.formDialog[size];
  const bodyPadding = variant === "form" ? formRecipe.bodyPadding : recipe.padding;
  const modalWidth = variant === "form" ? `min(${dialogWidths[width]}px, calc(100vw - 48px))` : dialogWidths[width];
  const formVariables = variant === "form" ? {
    "--vp-form-label-min-width": `${formRecipe.inlineLabelMinWidth}px`,
    "--vp-form-dialog-row-gap": `${formRecipe.contentGap}px`,
    "--vp-form-dialog-section-gap": `${formRecipe.contentGap * 2}px`,
    "--vp-form-dialog-item-margin": "0px",
  } as CSSProperties : {};
  const modalTitle = description === undefined ? title : <div className="vp-antd-dialog-title"><div>{title}</div><Typography.Text type="secondary" style={{ display: "block", marginTop: 4, fontSize: componentSizeRecipes.control[size].fontSize, fontWeight: 400, lineHeight: 1.5 }}>{description}</Typography.Text></div>;
  return <Modal open={open} title={modalTitle} footer={footer ?? null} className={variant === "form" ? "vp-antd-form-dialog" : undefined} width={modalWidth} onCancel={onClose} destroyOnHidden styles={{
    container: { ...dialogFrameStyle(height), display: "flex", flexDirection: "column", overflow: "hidden", ...formVariables },
    body: { ...dialogBodyStyle(contentOverflow), padding: bodyPadding, ...(scrollable ? { flex: "1 1 auto" } : {}) },
    header: { flex: "0 0 auto" },
    footer: { flex: "0 0 auto", marginTop: 0 },
  }}><ComponentSizeProvider size={size}>{children}</ComponentSizeProvider></Modal>;
}

export function Drawer({ open, title, children, footer, width = "md", size: requestedSize, placement = "right", onClose }: DrawerProps) {
  const horizontal = placement === "left" || placement === "right";
  const size = useComponentSize(requestedSize);
  return <AntdDrawer open={open} title={title} footer={footer} placement={placement} width={horizontal ? dialogWidths[width] : undefined} height={horizontal ? undefined : dialogWidths[width]} onClose={onClose} destroyOnHidden styles={{ body: { padding: componentSizeRecipes.layout[size].padding } }}><ComponentSizeProvider size={size}>{children}</ComponentSizeProvider></AntdDrawer>;
}

export function EmptyState({ title, description, size: requestedSize }: EmptyStateProps) {
  const size = useComponentSize(requestedSize);
  return <Empty description={<div style={{ fontSize: componentSizeRecipes.control[size].fontSize }}><strong>{title}</strong>{description === undefined ? null : <div>{description}</div>}</div>} />;
}

export function ErrorState({ title, retry, size: requestedSize }: ErrorStateProps) {
  const i18n = usePortalI18n();
  const size = useComponentSize(requestedSize);
  return <Alert type="error" showIcon message={title} action={retry === undefined ? undefined : <Button size={size} onClick={retry}>{i18n.text(message(namespace, "action.retry", "重试"))}</Button>} />;
}

export function Skeleton({ rows = 3, size: requestedSize }: SkeletonProps) { const size = useComponentSize(requestedSize); return <div style={{ fontSize: componentSizeRecipes.control[size].fontSize }}><AntdSkeleton active paragraph={{ rows }} /></div>; }
export function Busy({ label, size: requestedSize }: BusyProps) {
  const size = useComponentSize(requestedSize);
  const spinSize = size === "xs" || size === "sm" ? "small" : size === "lg" ? "large" : "default";
  return <Spin size={spinSize} tip={label}><span /></Spin>;
}
