import { Alert, Button as AntdButton, Drawer as AntdDrawer, Empty, Modal, Skeleton as AntdSkeleton, Spin, Tooltip } from "antd";
import type { ComponentType } from "react";
import type { BusyProps, ButtonProps, DialogProps, DrawerProps, EmptyStateProps, ErrorStateProps, IconButtonProps, SkeletonProps, VastPlanIconProps } from "@vastplan/ui-primitives";
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

export function iconButtonWith(Icon: ComponentType<VastPlanIconProps>, { icon, label, size: requestedSize, onClick, disabled, loading, tone = "normal" }: IconButtonProps) {
  const size = useComponentSize(requestedSize);
  const recipe = componentSizeRecipes.iconButton[size];
  return <Tooltip title={label}><AntdButton
    aria-label={label}
    type={tone === "primary" ? "primary" : "text"}
    danger={tone === "danger"}
    size={antdComponentSize[size]}
    icon={<Icon name={icon} size={size} style={{ width: recipe.iconEdge, height: recipe.iconEdge }} />}
    loading={loading}
    disabled={disabled}
    onClick={onClick}
    style={{ width: recipe.edge, height: recipe.edge, borderRadius: recipe.radius }}
  /></Tooltip>;
}

export function IconButton(props: IconButtonProps) { return iconButtonWith(VastPlanIcon, props); }

export function Dialog({ open, title, children, footer, width = "md", size: requestedSize, height, contentOverflow = "scroll", onClose }: DialogProps) {
  const scrollable = contentOverflow === "scroll";
  const size = useComponentSize(requestedSize);
  const recipe = componentSizeRecipes.layout[size];
  return <Modal open={open} title={title} footer={footer ?? null} width={dialogWidths[width]} onCancel={onClose} destroyOnHidden styles={{
    container: { ...dialogFrameStyle(height), display: "flex", flexDirection: "column", overflow: "hidden" },
    body: { ...dialogBodyStyle(contentOverflow), padding: recipe.padding, ...(scrollable ? { flex: "1 1 auto" } : {}) },
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
