import { useState } from "react";
import type { ComponentSize, PopoverPlacement, SemanticIconName } from "@vastplan/ui-primitives";
import { componentSizeRecipes, usePortalUI } from "@vastplan/ui-primitives";

export interface ActionMenuEntry {
  id: string;
  label: string;
  icon: SemanticIconName;
  disabled?: boolean;
  tone?: "normal" | "danger";
}

/**
 * Governed overflow menu shared by Workbench patterns. Callers provide action
 * data only; the component owns compact layout, focus, closing, and truncation.
 */
export function ActionMenuPopover({ label, items, triggerSize = "sm", menuSize = "sm", iconSize = menuSize, placement = "bottom-end", onSelect }: {
  label: string;
  items: readonly ActionMenuEntry[];
  triggerSize?: ComponentSize;
  menuSize?: ComponentSize;
  /** Menu action glyphs follow the same governed scale as their trigger. */
  iconSize?: ComponentSize;
  placement?: PopoverPlacement;
  onSelect(id: string): void;
}) {
  const ui = usePortalUI();
  const [open, setOpen] = useState(false);
  const iconEdge = componentSizeRecipes.iconButton[iconSize].iconEdge;
  if (items.length === 0) return null;
  return <ui.Popover
    open={open}
    placement={placement}
    surface="compact"
    ariaLabel={label}
    initialFocus="first"
    onOpenChange={setOpen}
    trigger={(props) => <span
      ref={(node) => props.ref(node?.querySelector<HTMLElement>("button") ?? node)}
      aria-expanded={props["aria-expanded"]}
      aria-controls={props["aria-controls"]}
      onClick={props.onClick}
      onKeyDown={props.onKeyDown}
      style={{ display: "inline-flex", lineHeight: 0 }}
    ><ui.IconButton icon="more" label={label} size={triggerSize} /></span>}
  ><ui.Menu
    size={menuSize}
    variant="action"
    items={items.map((item) => {
      const color = item.disabled === true
        ? ui.theme.tokens.color.mutedText
        : item.tone === "danger" ? ui.theme.tokens.color.danger : undefined;
      return {
        id: item.id,
        disabled: item.disabled,
        icon: <span style={{ display: "inline-flex", color }}><ui.Icon name={item.icon} size={iconSize} style={{ width: iconEdge, height: iconEdge }} /></span>,
        label: <span title={item.label} style={{ display: "block", maxWidth: 216, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", color }}>{item.label}</span>,
      };
    })}
    onSelect={(id) => {
      const item = items.find((candidate) => candidate.id === id && candidate.disabled !== true);
      if (item === undefined) return;
      setOpen(false);
      onSelect(item.id);
    }}
  /></ui.Popover>;
}
