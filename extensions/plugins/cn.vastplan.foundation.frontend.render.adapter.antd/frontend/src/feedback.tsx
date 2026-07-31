import { Alert, Button as AntdButton, Drawer as AntdDrawer, Empty, Modal, Skeleton as AntdSkeleton, Spin, Tooltip } from "antd";
import type { ComponentType } from "react";
import type { ButtonProps, DialogProps, DrawerProps, IconButtonProps, VastPlanIconProps } from "@vastplan/ui-primitives";
import { componentSizeRecipes, dialogBodyStyle, dialogFrameStyle, message, usePortalI18n, VastPlanIcon } from "@vastplan/ui-primitives";
import { antdComponentSize } from "./component-size";
import { dialogWidths, namespace } from "./theme";

function buttonStyle(kind: ButtonProps["kind"]): { type?: "primary" | "default" | "text"; danger?: boolean } {
  if (kind === "primary") return { type: "primary" };
  if (kind === "danger") return { type: "default", danger: true };
  if (kind === "text") return { type: "text" };
  return { type: "default" };
}

export function Button({ children, kind, size = "md", ...props }: ButtonProps) {
  const recipe = componentSizeRecipes.control[size];
  return <AntdButton {...buttonStyle(kind)} size={antdComponentSize[size]} style={{ height: recipe.height, paddingInline: recipe.inlinePadding, borderRadius: recipe.radius, fontSize: recipe.fontSize }} {...props}>{children}</AntdButton>;
}

export function iconButtonWith(Icon: ComponentType<VastPlanIconProps>, { icon, label, size = "md", onClick, disabled, loading, tone = "normal" }: IconButtonProps) {
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

export function Dialog({ open, title, children, footer, width = "md", height, contentOverflow = "scroll", onClose }: DialogProps) {
  const scrollable = contentOverflow === "scroll";
  return <Modal open={open} title={title} footer={footer ?? null} width={dialogWidths[width]} onCancel={onClose} destroyOnHidden styles={{
    container: { ...dialogFrameStyle(height), display: "flex", flexDirection: "column", overflow: "hidden" },
    body: { ...dialogBodyStyle(contentOverflow), ...(scrollable ? { flex: "1 1 auto" } : {}) },
  }}>{children}</Modal>;
}

export function Drawer({ open, title, children, footer, width = "md", placement = "right", onClose }: DrawerProps) {
  const horizontal = placement === "left" || placement === "right";
  return <AntdDrawer open={open} title={title} footer={footer} placement={placement} width={horizontal ? dialogWidths[width] : undefined} height={horizontal ? undefined : dialogWidths[width]} onClose={onClose} destroyOnHidden>{children}</AntdDrawer>;
}

export function EmptyState({ title, description }: { title: string; description?: string }) {
  return <Empty description={<><strong>{title}</strong>{description === undefined ? null : <div>{description}</div>}</>} />;
}

export function ErrorState({ title, retry }: { title: string; retry?(): void }) {
  const i18n = usePortalI18n();
  return <Alert type="error" showIcon message={title} action={retry === undefined ? undefined : <AntdButton onClick={retry}>{i18n.text(message(namespace, "action.retry", "重试"))}</AntdButton>} />;
}

export function Skeleton({ rows = 3 }: { rows?: number }) { return <AntdSkeleton active paragraph={{ rows }} />; }
export function Busy({ label }: { label?: string }) { return <Spin tip={label}><span /></Spin>; }
