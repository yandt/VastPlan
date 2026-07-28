import { Breadcrumb as AntdBreadcrumb, Button, Empty, Input, List, Menu as AntdMenu, Modal, Popover as AntdPopover, Tabs as AntdTabs, Typography } from "antd";
import type { MenuProps as AntdMenuProps } from "antd";
import { useEffect, useId, useRef } from "react";
import type { CSSProperties, KeyboardEvent, ReactNode } from "react";
import type { CommandItem, MenuItem, MenuProps, PopoverProps, RecordNavigationListProps, RecordTreeProps, TabsProps } from "@vastplan/ui-primitives";
import { componentSizeRecipes, componentVariantRecipes, message, usePortalI18n } from "@vastplan/ui-primitives";
import { namespace } from "./theme";
import { antdComponentSize } from "./component-size";

function menuItems(items: MenuItem[], size: NonNullable<MenuProps["size"]>, variant: NonNullable<MenuProps["variant"]>, onSelect?: (id: string) => void, parentDisabled = false): NonNullable<AntdMenuProps["items"]> {
  const recipe = componentSizeRecipes.menu[size];
  return items.map((item) => {
    const disabled = parentDisabled || item.disabled === true;
    const label = item.href === undefined ? item.label : <a href={item.href} onClick={(event) => {
      event.preventDefault();
      event.stopPropagation();
      if (!disabled) onSelect?.(item.id);
    }}>{item.label}</a>;
    return { key: item.id, label, icon: item.icon, disabled, style: { height: recipe.itemHeight, lineHeight: `${recipe.itemHeight}px`, margin: 0, paddingInline: recipe.itemInlinePadding, ...(variant === "action" ? componentVariantRecipes.menu.actionItem : {}) }, children: item.children?.length ? menuItems(item.children, size, variant, onSelect, disabled) : undefined };
  });
}

export function Menu({ items, activeID, size = "md", variant = "navigation", onSelect }: MenuProps) {
  const recipe = componentSizeRecipes.menu[size];
  const style: CSSProperties = {
    minWidth: recipe.minWidth,
    padding: recipe.surfacePadding,
    borderRadius: recipe.radius,
    ...(variant === "action" ? componentVariantRecipes.menu.action : {}),
    ["--ant-menu-item-height" as string]: `${recipe.itemHeight}px`,
  };
  return <AntdMenu selectedKeys={activeID === undefined ? [] : [activeID]} items={menuItems(items, size, variant, onSelect)} onClick={({ key }) => onSelect?.(key)} style={style} />;
}

export function Breadcrumb({ items }: { items: Array<{ id: string; label: string; href?: string; onSelect?(): void }> }) {
  return <AntdBreadcrumb items={items.map((item) => ({ key: item.id, title: item.href === undefined ? item.label : <a href={item.href} onClick={(event) => { event.preventDefault(); item.onSelect?.(); }}>{item.label}</a> }))} />;
}

export function Tabs({ items, activeID, size = "md", onChange }: TabsProps) {
  return <AntdTabs activeKey={activeID} size={antdComponentSize[size]} onChange={onChange} items={items.map((item) => ({ key: item.id, label: item.label, children: item.content, disabled: item.disabled }))} />;
}

export function CommandPalette({ open, commands, query, onQueryChange, onClose }: { open: boolean; commands: CommandItem[]; query: string; onQueryChange(query: string): void; onClose(): void }) {
  const i18n = usePortalI18n();
  const term = query.trim().toLocaleLowerCase();
  const visible = term === "" ? commands : commands.filter((command) => [command.title, command.description, ...(command.keywords ?? [])].some((value) => value?.toLocaleLowerCase().includes(term)));
  return <Modal open={open} title={i18n.text(message(namespace, "command.title", "命令"))} footer={null} onCancel={onClose} destroyOnHidden>
    <Input autoFocus value={query} placeholder={i18n.text(message(namespace, "command.search", "搜索命令"))} onChange={(event) => onQueryChange(event.target.value)} />
    {visible.length === 0 ? <Empty description={i18n.text(message(namespace, "command.empty", "没有匹配命令"))} /> : <List dataSource={visible} renderItem={(command) => <List.Item><Button type="text" block disabled={command.disabled} onClick={() => { command.run(); onClose(); }} style={{ height: "auto", textAlign: "left" }}><strong>{command.title}</strong>{command.description === undefined ? null : <Typography.Text type="secondary"> — {command.description}</Typography.Text>}</Button></List.Item>} />}
  </Modal>;
}

export function Popover({ open, trigger, children, placement = "bottom-start", surface = "default", initialFocus = "first", ariaLabel, onOpenChange }: PopoverProps) {
  const contentID = useId();
  const contentRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const focusInitial = () => {
    if (initialFocus === "none") return;
    const firstFocusable = "[role='menuitem']:not([aria-disabled='true']),button:not([disabled]),a[href],[tabindex]:not([tabindex='-1'])";
    const selector = initialFocus === "current" ? "[aria-current='page']" : firstFocusable;
    (contentRef.current?.querySelector<HTMLElement>(selector) ?? contentRef.current?.querySelector<HTMLElement>(firstFocusable))?.focus();
  };
  useEffect(() => { if (open) focusInitial(); }, [initialFocus, open]);
  const antdPlacement = ({ "bottom-start": "bottomLeft", bottom: "bottom", "bottom-end": "bottomRight", "top-start": "topLeft", top: "top", "top-end": "topRight" } as const)[placement];
  const close = (reason: "escape" | "outside") => { onOpenChange(false, reason); triggerRef.current?.focus(); };
  const triggerNode = trigger({
    ref: (node) => { triggerRef.current = node; }, "aria-expanded": open, "aria-controls": contentID,
    onClick: () => onOpenChange(!open, "trigger"),
    onKeyDown: (event) => {
      if (event.key === "ArrowDown") { event.preventDefault(); onOpenChange(true, "trigger"); }
      if (event.key === "Escape" && open) { event.preventDefault(); close("escape"); }
    },
  });
  return <AntdPopover open={open} trigger="click" placement={antdPlacement} styles={surface === "compact" ? { content: { padding: 4 } } : undefined} afterOpenChange={(next) => { if (next) focusInitial(); }} onOpenChange={(next) => { if (!next) close("outside"); }} content={<div id={contentID} ref={contentRef} role="region" aria-label={ariaLabel} onKeyDown={(event) => { if (event.key === "Escape") { event.preventDefault(); close("escape"); } }}>{children}</div>}>{triggerNode}</AntdPopover>;
}

export function RecordNavigationList({ items, selectedID, ariaLabel, onSelect }: RecordNavigationListProps) {
  return <div role="listbox" aria-label={ariaLabel} style={{ display: "grid", gap: 4 }}>{items.map((item) => <button key={item.id} type="button" role="option" aria-selected={item.id === selectedID} disabled={item.disabled} onClick={() => onSelect(item.id)} style={{ width: "100%", minHeight: 52, display: "grid", gridTemplateColumns: "minmax(0,1fr) auto", gap: 8, padding: "10px 12px", textAlign: "left", border: 0, borderRadius: 6, cursor: item.disabled ? "not-allowed" : "pointer", color: "var(--ant-color-text)", background: item.id === selectedID ? "var(--ant-color-primary-bg)" : "transparent" }}>
    <span style={{ minWidth: 0 }}><strong style={ellipsis}>{item.title}</strong>{item.description === undefined ? null : <span style={{ ...ellipsis, marginTop: 3, color: "var(--ant-color-text-secondary)" }}>{item.description}</span>}</span>{item.status}
  </button>)}</div>;
}

export function RecordTree({ items, selectedID, expandedIDs, ariaLabel, onSelect, onExpandedChange }: RecordTreeProps) {
  const expanded = new Set(expandedIDs);
  const toggle = (id: string) => onExpandedChange(expanded.has(id) ? expandedIDs.filter((value) => value !== id) : [...expandedIDs, id]);
  const branch = (nodes: RecordTreeProps["items"], level: number): ReactNode => <ul role={level === 1 ? "tree" : "group"} aria-label={level === 1 ? ariaLabel : undefined} style={{ listStyle: "none", margin: 0, padding: level === 1 ? 0 : "0 0 0 20px" }}>{nodes.map((item) => {
    const children = item.children ?? [];
    const open = expanded.has(item.id);
    return <li key={item.id} role="treeitem" aria-level={level} aria-selected={item.id === selectedID} aria-expanded={children.length === 0 ? undefined : open}>
      <div style={{ display: "grid", gridTemplateColumns: "32px minmax(0,1fr) auto", alignItems: "center", borderRadius: 6, background: item.id === selectedID ? "var(--ant-color-primary-bg)" : "transparent" }}>
        {children.length === 0 ? <span /> : <button type="button" aria-label={open ? "Collapse" : "Expand"} onClick={() => toggle(item.id)} style={treeButton}>{open ? "▾" : "▸"}</button>}
        <button data-record-tree-select type="button" disabled={item.disabled} onClick={() => onSelect(item.id)} onKeyDown={(event) => treeKeyboard(event, children.length > 0, open, () => toggle(item.id))} style={{ ...treeButton, minHeight: 44, textAlign: "left", color: "var(--ant-color-text)" }}><strong>{item.title}</strong>{item.description === undefined ? null : <span style={{ display: "block", color: "var(--ant-color-text-secondary)" }}>{item.description}</span>}</button>{item.status}
      </div>{children.length > 0 && open ? branch(children, level + 1) : null}
    </li>;
  })}</ul>;
  return branch(items, 1);
}

function treeKeyboard(event: KeyboardEvent<HTMLButtonElement>, hasChildren: boolean, open: boolean, toggle: () => void) {
  if ((event.key === "ArrowRight" && hasChildren && !open) || (event.key === "ArrowLeft" && hasChildren && open)) { event.preventDefault(); toggle(); return; }
  if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
  const buttons = [...(event.currentTarget.closest('[role="tree"]')?.querySelectorAll<HTMLButtonElement>("[data-record-tree-select]") ?? [])];
  const target = buttons[buttons.indexOf(event.currentTarget) + (event.key === "ArrowDown" ? 1 : -1)];
  if (target !== undefined) { event.preventDefault(); target.focus(); }
}

const ellipsis: React.CSSProperties = { display: "block", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
const treeButton: React.CSSProperties = { border: 0, background: "transparent", cursor: "pointer" };
